import { existsSync, readFileSync } from 'fs';
import { dirname, join, resolve } from 'path';
import { CfnOutput, Duration, SecretValue, Stack, StackProps } from 'aws-cdk-lib';
import { Vpc } from 'aws-cdk-lib/aws-ec2';
import { DockerImageAsset, Platform } from 'aws-cdk-lib/aws-ecr-assets';
import { Cluster, HelmChart, KubernetesVersion, ServiceAccount } from 'aws-cdk-lib/aws-eks';
import { Runtime } from 'aws-cdk-lib/aws-lambda';
import { SqsEventSource } from 'aws-cdk-lib/aws-lambda-event-sources';
import { NodejsFunction } from 'aws-cdk-lib/aws-lambda-nodejs';
import { Asset } from 'aws-cdk-lib/aws-s3-assets';
import { IQueue, Queue, QueueEncryption } from 'aws-cdk-lib/aws-sqs';
import { App, Chart } from 'cdk8s';
import { Deployment as KubeDeployment, ServiceAccount as KubeServiceAccount, Secret as KubeSecret } from 'cdk8s-plus-23';
import { Construct } from 'constructs';
import { Lumigo } from '@lumigo/cdk-constructs-v2';
import { Lumigo as LumigoResource } from '../imports/operator.lumigo.io';

export class EksPythonSqsLambdaStack extends Stack {

  constructor(scope: Construct, id: string, props: StackProps) {
    super(scope, id, props);

    const queue: IQueue = new Queue(this, 'SqsItestQueue', {
      encryption: QueueEncryption.KMS_MANAGED,
    });

    const handler = new NodejsFunction(this, 'SqsITestLambda', {
      runtime: Runtime.NODEJS_16_X,
      entry: join(__dirname, 'lambda', 'index.ts'),
      handler: 'handler',
    });
    handler.addEventSource(
      new SqsEventSource(queue, {
        batchSize: 10,
        maxBatchingWindow: Duration.seconds(3),
      }),
    );

    new Lumigo({
      lumigoToken: SecretValue.secretsManager('AccessKeys', { jsonField: 'LumigoToken' }),
    }).traceLambda(handler);

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
    const projectRoot = getProjectRoot();
    
    const lumigoManagerImageAsset = new DockerImageAsset(this, 'LumigoManagerImage', {
      directory: projectRoot,
      file: 'Dockerfile.controller',
      platform: Platform.LINUX_AMD64,
      exclude: ['tests'], // Avoid recursive inclusion in Docker build context
    });

    const lumigoTelemetryProxyImageAsset = new DockerImageAsset(this, 'LumigoTelemetryProxyImage', {
      directory: projectRoot,
      file: 'Dockerfile.proxy',
      platform: Platform.LINUX_AMD64,
      exclude: ['tests'], // Avoid recursive inclusion in Docker build context
      buildArgs: {
        lumigo_otel_collector_release: readFileSync(join(projectRoot, 'telemetryproxy', 'VERSION.otelcontibcol')).toString(),
      },
    });

    // The EKS cluster's NodeInstanceRole needs to be granted pull from the ECR repo
    // https://docs.aws.amazon.com/AmazonECR/latest/userguide/ECR_on_EKS.html
    lumigoManagerImageAsset.repository.grantPull(cluster.defaultNodegroup!.role);
    lumigoTelemetryProxyImageAsset.repository.grantPull(cluster.defaultNodegroup!.role);

    const lumigoOperatorNamespace = 'lumigo-system';

    const lumigoOperatorChart = new HelmChart(this, 'lumigo-operator', {
      chartAsset: new Asset(this, 'lumigo-operator-chart-asset', {
        path: join(projectRoot, 'charts', 'lumigo-operator'),
      }),
      cluster,
      createNamespace: true,
      namespace: lumigoOperatorNamespace,
      release: 'test',
      values: {
        controllerManager: {
          manager: {
            image: {
              repository: lumigoManagerImageAsset.repository.repositoryUri,
              tag: lumigoManagerImageAsset.imageTag,
            },
          },
          telemetryProxy: {
            image: {
              repository: lumigoTelemetryProxyImageAsset.repository.repositoryUri,
              tag: lumigoTelemetryProxyImageAsset.imageTag,
            },
          },
        },
      },
      wait: true,
    });

    /**
     * Deploy test app
     */
    const testAppChart = new Chart(new App(), `${cluster.clusterName}-test-app`, {});

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
        token: SecretValue.secretsManager('AccessKeys').toJSON().LumigoToken,
      },
    });

    const lumigoInstance = new LumigoResource(testAppChart, 'lumigo-instance', {
      metadata: {
        name: 'lumigo',
        namespace: testAppNamespace,
      },
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
    lumigoInstance.addDependency(lumigoSecret);

    const testAppDeployment = new KubeDeployment(testAppChart, 'TestAppDeployment', {
      metadata: {
        name: 'test-app',
        namespace: testAppNamespace,
      },
      replicas: 2,
      serviceAccount: KubeServiceAccount.fromServiceAccountName(testAppChart, 'service-account', testAppServiceAccount.serviceAccountName, {
        namespaceName: testAppNamespace,
      }),
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
        securityContext: {
          ensureNonRoot: true,
          privileged: false,
          allowPrivilegeEscalation: false,
          readOnlyRootFilesystem: true,
          user: 1234
        }
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

    new CfnOutput(this, 'lumigo_operator_manager_image', {
      value: `${lumigoManagerImageAsset.imageUri}:${lumigoManagerImageAsset.imageTag}`,
    });

    new CfnOutput(this, 'lumigo_operator_telemetry_proxy_image', {
      value: `${lumigoTelemetryProxyImageAsset.imageUri}:${lumigoTelemetryProxyImageAsset.imageTag}`,
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