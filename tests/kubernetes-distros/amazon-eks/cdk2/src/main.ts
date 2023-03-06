import { App } from 'aws-cdk-lib';
import { EksPythonSqsLambdaStack } from './eks-python-sqs-lambda/eks-python-sqs-lambda-stack'

const env = {
  account: process.env.CDK_DEFAULT_ACCOUNT,
  region: process.env.CDK_DEFAULT_REGION,
};

const app = new App();

new EksPythonSqsLambdaStack(app, 'lumigo-operator-itest', {
  env,
});

app.synth();