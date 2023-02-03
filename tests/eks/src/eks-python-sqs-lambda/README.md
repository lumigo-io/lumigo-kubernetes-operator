# EKS End-to-End Integration Tests

## Setup

### Software to have on hand

1. Node.js v14+ installed
1. Yarn installed
1. A local Docker daemon installed that can run `docker build` as the user that you are running the commands in the [Run](#run) section
1. AWS CLI ([installation instructions](https://docs.aws.amazon.com/cli/latest/userguide/cli-chap-welcome.html))
1. Local setup of the AWS CLI:
   ```sh
   aws configure
   ```
1. AWS Cloud Development Kit (CDK) v2 ([installation instructions](https://docs.aws.amazon.com/cdk/v2/guide/getting_started.html))

### AWS account

1. AWS Cloud Development Kit (CDK) v2 bootstrapped:
   ```sh
   cdk bootstrap
   ```
1. Create an AWS Secret Manager secret called `AccessKeys`, with `LumigoToken` as a field, and the actual token as its value.

## Run

Install all subdirectories dependencies
```sh
find . -name node_modules -prune -o -name package.json -execdir npm install \; && rm -rf package-lock.json
```

```
yarn install
npm run build
cdk deploy --all
```

To deploy specific stack with AWS profile:

```sh
CDK_DEFAULT_ACCOUNT=<AWS account number> cdk deploy ecs-js-http-ecs-js --profile <AWS cli profile>
```

## Connect to EKS

IAM with Amazon EKS is _difficult_.

### Kubectl

Only someone with the **creator role** for EKS can generate a configuration for `kubeconfig` that can access the cluster.
The solution is to create a trust relation between the CreatorRole for the cluster and your user's principal:

1. Open the `<eks_cluster_creator_role>` provided by the CDK in the AWS Console IAM Roles view.
1. Click on "Trust relationships"
1. Click on "Edit trust policy"
1. Append the JSON snippet generated with the following command to the `Statements` JSON list:
   ```sh
   aws sts get-caller-identity | jq -r '.Arn | {"Effect":"Allow","Action":"sts:AssumeRole","Principal":{"AWS":.}}'
   ```
1. Get an updated `kubeconfig` configuration with:
   ```sh
   aws eks update-kubeconfig --region <aws_region> --name <eks_cluster_name> --role-arn <eks_cluster_creator_role>
   ```

To validate, run:

```sh
kubeconfig get nodes
```

### Helm

```sh
aws sts assume-role --role-arn <eks_cluster_creator_role> --role-session-name dev
```

Then you can proceed with Helm-ing as normal.

## Other useful commands

* `npm run watch`   watch for changes and compile
* `npm run test`    perform the jest unit tests
* `cdk diff`        compare deployed stack with current state
* `cdk synth`       emits the synthesized CloudFormation template
