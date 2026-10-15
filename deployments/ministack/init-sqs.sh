#!/bin/bash
set -e

echo "=== Initializing SQS FIFO Queues ==="

export AWS_ACCESS_KEY_ID="${AWS_ACCESS_KEY_ID:-test}"
export AWS_SECRET_ACCESS_KEY="${AWS_SECRET_ACCESS_KEY:-test}"
export AWS_DEFAULT_REGION="${AWS_REGION:-us-east-1}"

ENDPOINT_URL="${AWS_ENDPOINT_URL:-http://localhost:4566}"
AWS_CMD="aws --endpoint-url=${ENDPOINT_URL} --region ${AWS_DEFAULT_REGION}"

if command -v awslocal >/dev/null 2>&1; then
    AWS_CMD="awslocal --region ${AWS_DEFAULT_REGION}"
fi

echo "Creating wager-transactions-dlq.fifo..."
$AWS_CMD sqs create-queue \
    --queue-name wager-transactions-dlq.fifo \
    --attributes FifoQueue=true,ContentBasedDeduplication=false || true

echo "Creating wager-events.fifo..."
$AWS_CMD sqs create-queue \
    --queue-name wager-events.fifo \
    --attributes FifoQueue=true,ContentBasedDeduplication=false || true

echo "Creating wager-transactions.fifo..."
$AWS_CMD sqs create-queue \
    --queue-name wager-transactions.fifo \
    --attributes FifoQueue=true,ContentBasedDeduplication=false || true

DLQ_ARN=$($AWS_CMD sqs get-queue-attributes \
    --queue-url "${ENDPOINT_URL}/000000000000/wager-transactions-dlq.fifo" \
    --attribute-names QueueArn \
    --query 'Attributes.QueueArn' \
    --output text 2>/dev/null || echo "arn:aws:${AWS_DEFAULT_REGION}:000000000000:wager-transactions-dlq.fifo")

echo "DLQ ARN: $DLQ_ARN"

$AWS_CMD sqs set-queue-attributes \
    --queue-url "${ENDPOINT_URL}/000000000000/wager-transactions.fifo" \
    --attributes "{\"RedrivePolicy\":\"{\\\"deadLetterTargetArn\\\":\\\"${DLQ_ARN}\\\",\\\"maxReceiveCount\\\":\\\"5\\\"}\"}" || true

echo "=== SQS FIFO Queues Initialized Successfully ==="
