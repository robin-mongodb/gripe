# RDS PostgreSQL — the plain engine, not Aurora. Single AZ, no multi-AZ, no read replica.
# Cheapest thing that actually holds real data.

resource "aws_db_subnet_group" "pg" {
  name       = "${var.project}-pg"
  subnet_ids = data.aws_subnets.default.ids
}

resource "aws_db_instance" "pg" {
  identifier              = "${var.project}-pg"
  engine                  = "postgres"
  engine_version          = var.pg_version
  instance_class          = var.pg_instance_class
  allocated_storage       = var.pg_allocated_storage_gb
  storage_type            = "gp3"
  storage_encrypted       = true
  db_name                 = var.pg_database
  username                = var.pg_username
  password                = var.pg_password
  publicly_accessible     = false
  db_subnet_group_name    = aws_db_subnet_group.pg.name
  vpc_security_group_ids  = [aws_security_group.rds.id]
  skip_final_snapshot     = true # ponytail: fine for a demo; flip to false in prod.
  deletion_protection     = false
  backup_retention_period = 1 # ponytail: 1 day is enough for the demo.
  apply_immediately       = true
}
