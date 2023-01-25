from diagrams import Cluster, Diagram, Edge
from diagrams.aws.compute import EKS
from diagrams.aws.compute import Lambda
from diagrams.aws.integration import SQS
from diagrams.k8s.compute import Deployment, Pod

# For examples on how to change this diagram, have a look at:
#   https://diagrams.mingrammer.com/
with Diagram("Test Diagram", show=False):
    with Cluster("Kubernetes"):
        pod = Pod("test-app")
        deployment = Deployment("test-app")

        deployment >> pod

        k8s_resources = [deployment, pod]

    k8s_resources - EKS("SqsITestCluster")

    sqs = SQS("SqsITestQueue")
    sqs >> Edge(label='SqsEventSource') >> Lambda("SqsITestLambda")

    pod >> Edge(label='boto3.Client.send_message every 10 seconds') >> sqs