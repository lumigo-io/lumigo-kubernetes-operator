const { awscdk } = require('projen');
const project = new awscdk.AwsCdkTypeScriptApp({
  cdkVersion: '2.61.1',
  defaultReleaseBranch: 'main',
  name: 'lumigo-operator-itest-eks',
  deps: ['cdk8s', 'cdk8s-plus-23'],
  devDeps: ['@types/aws-lambda', 'esbuild'],
  github: false,
});
project.synth();