import boto3
import os

bucket_name = os.getenv("S3_BUCKET")

s3_client = boto3.client(
    "s3",
    endpoint_url="http://localhost:4566",
    aws_access_key_id=os.getenv("AWS_ACCESS_KEY_ID"),
    aws_secret_access_key=os.getenv("AWS_SECRET_ACCESS_KEY"),
)

s3_client.create_bucket(Bucket=bucket_name)