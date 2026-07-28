# Use the default VPC to keep this cheap and simple.
# ponytail: swap for a purpose-built VPC when the demo grows a second env.

data "aws_vpc" "default" {
  default = true
}

data "aws_subnets" "default" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.default.id]
  }
}

# EC2 security group — HTTP open to allowed_http_cidr, SSH open to allowed_ssh_cidr only.
resource "aws_security_group" "ec2" {
  name        = "${var.project}-ec2"
  description = "Gripe EC2: HTTP from allowed CIDR, SSH from your IP, all egress."
  vpc_id      = data.aws_vpc.default.id

  ingress {
    description = "HTTP"
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = [var.allowed_http_cidr]
  }

  ingress {
    description = "SSH"
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = [var.allowed_ssh_cidr]
  }

  egress {
    description = "All egress"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

# RDS security group — Postgres in from the EC2 SG only, no public access.
resource "aws_security_group" "rds" {
  name        = "${var.project}-rds"
  description = "Gripe RDS: 5432 from EC2 SG only."
  vpc_id      = data.aws_vpc.default.id

  ingress {
    description     = "PostgreSQL from EC2"
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = [aws_security_group.ec2.id]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}
