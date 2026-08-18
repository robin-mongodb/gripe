data "aws_ami" "al2023" {
  most_recent = true
  owners      = ["amazon"]
  filter {
    name   = "name"
    values = ["al2023-ami-*-x86_64"]
  }
  filter {
    name   = "architecture"
    values = ["x86_64"]
  }
}

resource "aws_key_pair" "this" {
  key_name   = "${var.project}-ec2"
  public_key = var.ssh_pubkey
}

# --- IAM: EC2 can talk to its three SQS queues, nothing else ---
data "aws_iam_policy_document" "assume_ec2" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ec2.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "ec2" {
  name               = "${var.project}-ec2"
  assume_role_policy = data.aws_iam_policy_document.assume_ec2.json
}

data "aws_iam_policy_document" "sqs_access" {
  statement {
    actions = [
      "sqs:SendMessage",
      "sqs:ReceiveMessage",
      "sqs:DeleteMessage",
      "sqs:GetQueueAttributes",
      "sqs:GetQueueUrl",
      "sqs:ChangeMessageVisibility",
    ]
    resources = [
      aws_sqs_queue.payment_created.arn,
      aws_sqs_queue.fraud.arn,
      aws_sqs_queue.fee.arn,
    ]
  }
}

resource "aws_iam_role_policy" "sqs" {
  name   = "${var.project}-sqs"
  role   = aws_iam_role.ec2.id
  policy = data.aws_iam_policy_document.sqs_access.json
}

resource "aws_iam_role_policy_attachment" "ssm" {
  # Optional: lets you use SSM Session Manager for shell access without exposing SSH.
  role       = aws_iam_role.ec2.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_instance_profile" "ec2" {
  name = "${var.project}-ec2"
  role = aws_iam_role.ec2.name
}

# --- Render .env content the user-data script writes to disk ---
locals {
  # Base env sourced from Terraform outputs (RDS + SQS wired in for free).
  base_env = {
    GRIPE_BACKEND           = "mongo"
    API_ADDR                = ":8080"
    AWS_REGION              = var.aws_region
    # Both DSNs hit the writer: Mongo reads primary by default, so reading the
    # PG standbys would skew the comparison. reader_endpoint exists if wanted.
    PG_WRITER_DSN           = "postgres://${var.pg_username}:${var.pg_password}@${aws_rds_cluster.pg.endpoint}:5432/${var.pg_database}?sslmode=require"
    PG_READER_DSN           = "postgres://${var.pg_username}:${var.pg_password}@${aws_rds_cluster.pg.endpoint}:5432/${var.pg_database}?sslmode=require"
    SQS_PAYMENT_CREATED_URL = aws_sqs_queue.payment_created.url
    SQS_FRAUD_URL           = aws_sqs_queue.fraud.url
    SQS_FEE_URL             = aws_sqs_queue.fee.url
  }
  # Merge caller-provided extras (MONGO_URI/MONGO_DB, etc.) on top.
  merged_env = merge(local.base_env, var.app_env)
  env_file   = join("\n", [for k, v in local.merged_env : "${k}=${v}"])
}

resource "aws_instance" "app" {
  ami                    = data.aws_ami.al2023.id
  instance_type          = var.ec2_instance_type
  subnet_id              = data.aws_subnets.default.ids[0]
  key_name               = aws_key_pair.this.key_name
  vpc_security_group_ids = [aws_security_group.ec2.id]
  iam_instance_profile   = aws_iam_instance_profile.ec2.name

  user_data = templatefile("${path.module}/user-data.sh", {
    repo_url = var.repo_url
    env_file = local.env_file
  })

  root_block_device {
    volume_size = 30 # AMI snapshot minimum; 20 fails with InvalidBlockDeviceMapping
    volume_type = "gp3"
    encrypted   = true
  }

  tags = {
    Name = "${var.project}-app"
  }

  # ponytail: user-data changes require replacement to re-run — noisy for iteration.
  # Use `gripe-redeploy` on the box for code changes; use `taint` for a real re-bootstrap.
  lifecycle {
    ignore_changes = [user_data]
  }
}
