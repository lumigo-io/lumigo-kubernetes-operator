import { existsSync } from 'fs';
import { dirname, join, resolve } from 'path';
import { URL } from 'url';
import { CfnOutput, Duration, SecretValue, Stack, StackProps } from 'aws-cdk-lib';
import { Vpc } from 'aws-cdk-lib/aws-ec2';
import { DockerImageAsset, Platform } from 'aws-cdk-lib/aws-ecr-assets';
import { Cluster, KubernetesVersion, ServiceAccount } from 'aws-cdk-lib/aws-eks';
import { LayerVersion, Runtime } from 'aws-cdk-lib/aws-lambda';
import { SqsEventSource } from 'aws-cdk-lib/aws-lambda-event-sources';
import { NodejsFunction } from 'aws-cdk-lib/aws-lambda-nodejs';
import { IQueue, Queue, QueueEncryption } from 'aws-cdk-lib/aws-sqs';
import { App, Chart, Helm } from 'cdk8s';
import { Deployment as KubeDeployment, Secret as KubeSecret } from 'cdk8s-plus-23';
import { Lumigo } from '../imports/operator.lumigo.io';
import { Construct } from 'constructs';

export interface EksStackProps extends StackProps {
  readonly clusterName: string;
  readonly lumigoEndpoint: URL;
}

export class EksPythonSqsLambdaStack extends Stack {

  constructor(scope: Construct, id: string, props: EksStackProps) {
    super(scope, id, props);

    const queue: IQueue = new Queue(this, 'SqsItestQueue', {
      encryption: QueueEncryption.KMS_MANAGED,
    });

    const handler = new NodejsFunction(this, 'SqsITestLambda', {
      runtime: Runtime.NODEJS_16_X,
      entry: join(__dirname, 'lambda', 'index.ts'),
      handler: 'handler',
      environment: {
        LUMIGO_TRACER_TOKEN: SecretValue.secretsManager('AccessKeys', { jsonField: 'LumigoToken' }).toString(), // Pity we cannot mount secrets in the same way ECS can :-(
        LUMIGO_TRACER_HOST: props.lumigoEndpoint.hostname,
        AWS_LAMBDA_EXEC_WRAPPER: '/opt/lumigo_wrapper',
      },
      layers: [
        LayerVersion.fromLayerVersionArn(this, 'LumigoLayer', 'arn:aws:lambda:eu-central-1:114300393969:layer:lumigo-node-tracer:189'),
      ],
    });
    handler.addEventSource(
      new SqsEventSource(queue, {
        batchSize: 10,
        maxBatchingWindow: Duration.seconds(3),
      }),
    );

    const vpc = new Vpc(this, 'EksPythonSqsLambdaStackVpc');

    const cluster = new Cluster(this, 'Cluster', {
      clusterName: 'EksPythonSqsLambdaCluster',
      version: KubernetesVersion.V1_23,
      defaultCapacity: 1, // Just one node for the deployment test
      vpc,
    });

    /**
     * Deploy Lumigo Operator
     */
    const lumigoOperatorNamespace = 'lumigo-system';
    const lumigoOperatorNamespaceManifest = cluster.addManifest(lumigoOperatorNamespace, {
      apiVersion: 'v1',
      kind: 'Namespace',
      metadata: {
        name: lumigoOperatorNamespace,
      },
    });

    const projectRoot = getProjectRoot();

    const lumigoOperatorImageAsset = new DockerImageAsset(this, 'LumigoOperator', {
      directory: projectRoot,
      platform: Platform.LINUX_AMD64,
      exclude: ['tests'], // Avoid recursive inclusion in Docker build context
    });

    const lumigoOperatorChart = new Chart(new App(), `${props.clusterName}-lumigo-operator`, {});
    /* const lumigoOperatorHelmChart =*/ new Helm(lumigoOperatorChart, 'LumigoOperator', {
      chart: join(projectRoot, 'deploy', 'helm'),
      releaseName: 'test',
      namespace: lumigoOperatorNamespace,
      helmFlags: ['--wait'],
      values: {
        'controllerManager.manager.image.repository': lumigoOperatorImageAsset.repository.repositoryUri,
        'controllerManager.manager.image.tag': lumigoOperatorImageAsset.imageTag,
      }
    });

    // The EKS cluster's NodeInstanceRole needs to be granted pull from the ECR repo
    // https://docs.aws.amazon.com/AmazonECR/latest/userguide/ECR_on_EKS.html
    lumigoOperatorImageAsset.repository.grantPull(cluster.defaultNodegroup!.role);

    const lumigoOperatorChartManifest = cluster.addCdk8sChart('lumigo-operator', lumigoOperatorChart, {});
    lumigoOperatorChartManifest.node.addDependency(lumigoOperatorNamespaceManifest);

    /**
     * Deploy test app
     */
    const testAppChart = new Chart(new App(), `${props.clusterName}-test-app`, {});
    testAppChart.node.addDependency(lumigoOperatorChart);

    const testAppNamespace = 'test-app';
    const lumigoTokenSecretName = 'lumigo';

    // We create this with a manifest rather than the CDK8s chart so
    // that we can use it in the service account (which needs to be
    // built with the CDK API so that we can grant permissions to it
    // for the SQS queue)
    const testAppNamespaceManifest = cluster.addManifest(testAppNamespace, {
      apiVersion: 'v1',
      kind: 'Namespace',
      metadata: {
        name: testAppNamespace,
      },
    });

    const testAppServiceAccount = new ServiceAccount(this, 'test-app', {
      cluster: cluster,
      namespace: testAppNamespace,
      name: 'test-app',
    });
    // Avoid race condition between namespace and service account creation
    testAppServiceAccount.node.addDependency(testAppNamespaceManifest);

    const testAppImageAsset = new DockerImageAsset(this, 'EksPythonSqsApp', {
      directory: __dirname + '/container',
      platform: Platform.LINUX_AMD64,
    });
    // The EKS cluster's NodeInstanceRole needs to be granted pull from the ECR repo
    // https://docs.aws.amazon.com/AmazonECR/latest/userguide/ECR_on_EKS.html
    testAppImageAsset.repository.grantPull(cluster.defaultNodegroup!.role);

    const lumigoSecret = new KubeSecret(testAppChart, lumigoTokenSecretName, {
      metadata: {
        name: lumigoTokenSecretName,
        namespace: testAppNamespace,
      },
      stringData: {
        // Sigh, no integration with the CDK constructs yet :-(
        token: SecretValue.unsafePlainText('AccessKeys').toJSON().LumigoToken,
      },
    });

    const lumigoInstance = new Lumigo(testAppChart, 'lumigo-instance', {
      spec: {
        lumigoToken: {
          secretRef: {
            name: lumigoSecret.name,
            key: 'token',
          }
        },
        tracing: {
          injection: {
            enabled: true
          }
        }
      }
    });

    const testAppDeployment = new KubeDeployment(testAppChart, 'TestAppDeployment', {
      metadata: {
        name: 'test-app',
        namespace: testAppNamespace,
      },
      replicas: 2,
      containers: [{
          image: testAppImageAsset.imageUri,
          envVariables: {
            'OTEL_SERVICE_NAME': {
              value: 'SqsProducer',
            },
            'TARGET_QUEUE_URL': {
              value: queue.queueUrl!,
            },
            'AWS_REGION': {
              value: props.env?.region,
            },
            'OTEL_RESOURCE_ATTRIBUTES': {
              value: `aws.eks.cluster.arn=${cluster.clusterArn}`,
            },
          },
          ports: [{ number: 8080 }],
        }
      ]
    });
    queue.grantSendMessages(testAppServiceAccount);

    testAppDeployment.node.addDependency(lumigoSecret, lumigoInstance);

    const testAppChartManifest = cluster.addCdk8sChart('test-app', testAppChart, {});
    testAppChartManifest.node.addDependency(testAppServiceAccount, lumigoOperatorChart);

    new CfnOutput(this, 'aws_region', {
      value: props.env?.region || 'unknown_region',
    });

    new CfnOutput(this, 'eks_cluster_arn', {
      value: cluster.clusterArn,
    });

    new CfnOutput(this, 'eks_cluster_name', {
      value: cluster.clusterName,
    });

    new CfnOutput(this, 'eks_cluster_creator_role', {
      value: cluster.adminRole.roleArn,
    });

    new CfnOutput(this, 'lumigo_operator_controller_image_uri', {
      value: lumigoOperatorImageAsset.imageUri,
    });

    new CfnOutput(this, 'lumigo_operator_controller_image_tag', {
      value: lumigoOperatorImageAsset.imageTag,
    });
  }

}

function getProjectRoot(): string {
  let dir = __dirname;

  while (!existsSync(join(dir, 'PROJECT'))) {
    if (dir === '/') {
      throw new Error('Root of repository not found');
    }

    dir = dirname(dir);
  }

  return resolve(dir);
}