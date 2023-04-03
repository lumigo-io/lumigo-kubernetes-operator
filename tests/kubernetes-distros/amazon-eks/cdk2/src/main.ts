import { App, Stack, StackProps } from 'aws-cdk-lib';
import { CfnInclude } from 'aws-cdk-lib/cloudformation-include';
import { Construct } from 'constructs';
import { join } from 'path';
import { EksPythonSqsLambdaStack } from './eks-python-sqs-lambda/eks-python-sqs-lambda-stack'

class ManualStack extends Stack {
  constructor(scope: Construct, id: string, props: StackProps) {
    super(scope, id, props);

    new CfnInclude(this, 'Template', { 
      templateFile: join(__dirname, 'eks-python-sqs-lambda', 'manual.yaml'),
    });
  }
}

const env = {
  account: process.env.CDK_DEFAULT_ACCOUNT,
  region: process.env.CDK_DEFAULT_REGION,
};

const app = new App();

new EksPythonSqsLambdaStack(app, 'lumigo-operator-itest', {
  env,
});

new ManualStack(app, 'manual-lumigo-operator-itest', {
  env,
});

app.synth();