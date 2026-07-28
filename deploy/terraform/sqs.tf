# Three standard queues. Fan-out via SNS is overkill for one demo — the api
# publishes to each queue directly (see internal/api once wired).
# ponytail: swap to SNS+SQS if a third consumer joins.

resource "aws_sqs_queue" "payment_created" {
  name                       = "${var.project}-payment-created"
  visibility_timeout_seconds = 30
  message_retention_seconds  = 345600 # 4 days
}

resource "aws_sqs_queue" "fraud" {
  name                       = "${var.project}-fraud"
  visibility_timeout_seconds = 30
  message_retention_seconds  = 345600
}

resource "aws_sqs_queue" "fee" {
  name                       = "${var.project}-fee"
  visibility_timeout_seconds = 30
  message_retention_seconds  = 345600
}
