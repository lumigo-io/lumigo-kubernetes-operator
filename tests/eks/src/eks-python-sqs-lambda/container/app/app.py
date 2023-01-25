import boto3
import os
import time

from opentelemetry import trace

if not (aws_region := os.environ.get("AWS_REGION")):
    raise Exception("The required 'AWS_REGION' is not defined")

if not (queue_url := os.environ.get("TARGET_QUEUE_URL")):
    raise Exception("The required 'TARGET_URL' is not defined")

client = boto3.client('sqs', region_name=aws_region)
tracer = trace.get_tracer(__name__)

while True:
    # We create an internal root span, as this is effectively a
    # batch job.
    with tracer.start_as_current_span("sqs_root") as root_span:
        client.send_message(
            QueueUrl=queue_url,
            MessageBody=f"Hello from Boto3"
        )

    time.sleep(1)
