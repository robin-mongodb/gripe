output "app_public_ip" {
  value       = aws_instance.app.public_ip
  description = "Point your browser at http://<this>/ once user-data finishes (~2 min after apply)."
}

output "ssh_command" {
  value       = "ssh ec2-user@${aws_instance.app.public_ip}"
  description = "Copy-paste to shell into the box."
}

output "rds_endpoint" {
  value       = aws_rds_cluster.pg.endpoint
  description = "RDS Postgres cluster writer endpoint (private — only reachable from the EC2 SG)."
}

output "rds_reader_endpoint" {
  value       = aws_rds_cluster.pg.reader_endpoint
  description = "Load-balanced endpoint for the two readable standbys. Unused by the app (reads go to the writer for parity with Mongo primary reads)."
}

output "sqs_urls" {
  value = {
    payment_created = aws_sqs_queue.payment_created.url
    fraud           = aws_sqs_queue.fraud.url
    fee             = aws_sqs_queue.fee.url
  }
  description = "SQS queue URLs (already baked into the EC2's .env)."
}
