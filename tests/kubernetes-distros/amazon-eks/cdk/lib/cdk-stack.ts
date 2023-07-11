import { existsSync, readFileSync } from 'fs';
import { dirname, join, resolve } from 'path';
import * as cdk from 'aws-cdk-lib';
import { Construct } from 'constructs';
import { Vpc } from 'aws-cdk-lib/aws-ec2';
import { Cluster, HelmChart, KubernetesVersion } from 'aws-cdk-lib/aws-eks';
import { DockerImageAsset, Platform } from 'aws-cdk-lib/aws-ecr-assets';
import { Asset } from 'aws-cdk-lib/aws-s3-assets';
import { App, Chart } from 'cdk8s';
import { Deployment as KubeDeployment, Namespace as KubeNamespace, Secret as KubeSecret } from 'cdk8s-plus-26';
import { Lumigo as LumigoResource } from './imports/operator.lumigo.io';
import { KubectlV26Layer } from '@aws-cdk/lambda-layer-kubectl-v26';


export class EksOperatorTestCdkStack extends cdk.Stack {
  constructor(scope: Construct, id: string, props?: cdk.StackProps) {
    super(scope, id, props);
    const token = "t_186eb52a329344cdb0d6c";
    const endpoint = "https://mosheshaham-edge-app-us-west-2.mosheshaham.golumigo.com";

    const vpc = new Vpc(this, 'EksOperatorTestVpc');

    const cluster = new Cluster(this, 'EksOperatorTestEks', {
      clusterName: 'EksOperatorTestCluster',
      version: KubernetesVersion.V1_26,
      kubectlLayer: new KubectlV26Layer(this, 'Kubectlv26Layer'),
      defaultCapacity: 1, // Just one node for the deployment test
      vpc,
    });

    /**
     * Deploy Lumigo Operator
     */
    const projectRoot = getProjectRoot();
    console.log("projectRoot", projectRoot)

    const lumigoManagerImageAsset = new DockerImageAsset(this, 'LumigoManagerImage', {
      directory: `${projectRoot}/controller`,
      file: 'Dockerfile',
      platform: Platform.LINUX_AMD64,
      exclude: ['tests'], // Avoid recursive inclusion in Docker build context
    });

    const lumigoTelemetryProxyImageAsset = new DockerImageAsset(this, 'LumigoTelemetryProxyImage', {
      directory: `${projectRoot}/telemetryproxy`,
      file: 'Dockerfile',
      platform: Platform.LINUX_AMD64,
      exclude: ['tests'], // Avoid recursive inclusion in Docker build context
      buildArgs: {
        lumigo_otel_collector_release: readFileSync(join(projectRoot, 'VERSION')).toString(),
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
        endpoint: {
          otlp: {
            url: endpoint
          }
        },
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
    const testAppImageAsset = new DockerImageAsset(this, 'EksOperatorTestApp', {
      directory: __dirname + '/../express',
      platform: Platform.LINUX_AMD64,
    });
    testAppImageAsset.repository.grantPull(cluster.defaultNodegroup!.role);

    const testAppChart = new Chart(new App(), `chart-test-app`, {});

    const testAppNamespace = 'test-app';
    const lumigoTokenSecretName = 'lumigo';

    const namespace = new KubeNamespace(testAppChart, "test-app-namespace", {
      metadata: {
        name: testAppNamespace
      }
    });

    const lumigoSecret = new KubeSecret(testAppChart, "lumigo-secret-construct", {
      metadata: {
        name: lumigoTokenSecretName,
        namespace: testAppNamespace,
      },
      stringData: {
        token: token,
      },
    });
    lumigoSecret.node.addDependency(namespace)

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
      replicas: 1,
      containers: [{
        image: testAppImageAsset.imageUri,
        envVariables: {
          'OTEL_SERVICE_NAME': {
            value: 'TestService2',
          },
        },
        ports: [{ number: 3000 }],
        securityContext: {
          user: 1000
        }
      }
      ]
    });

    testAppDeployment.node.addDependency(lumigoSecret, lumigoInstance);

    const testAppChartManifest = cluster.addCdk8sChart('test-app', testAppChart, {});
    testAppChartManifest.node.addDependency(lumigoOperatorChart);

    new cdk.CfnOutput(this, 'eks_cluster_arn', {
      value: cluster.clusterArn,
    });

    new cdk.CfnOutput(this, 'eks_cluster_name', {
      value: cluster.clusterName,
    });

    new cdk.CfnOutput(this, 'eks_cluster_creator_role', {
      value: cluster.adminRole.roleArn,
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