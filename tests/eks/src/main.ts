import { App } from 'aws-cdk-lib';
import { EksPythonSqsLambdaStack } from './eks-python-sqs-lambda/eks-python-sqs-lambda-stack'

const env = {
  account: process.env.CDK_DEFAULT_ACCOUNT,
  region: process.env.CDK_DEFAULT_REGION,
};

const app = new App();

new EksPythonSqsLambdaStack(app, 'eks-python-sqs-lambda', {
  env,
  clusterName: 'OperatorITestCluster',
  lumigoEndpoint: new URL('https://ga-otlp.lumigo-tracer-edge.golumigo.com/'),
});

app.synth();