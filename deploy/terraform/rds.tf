# RDS PostgreSQL Multi-AZ DB cluster — 1 writer + 2 readable standbys across 3 AZs.
# This is the RDS analog of the 3-node Atlas M30 replica set: writes commit
# semi-synchronously to at least one standby (≈ Mongo w:majority).
# Not Aurora — plain PG engine, per the project constraint.

resource "aws_db_subnet_group" "pg" {
  name       = "${var.project}-pg"
  subnet_ids = data.aws_subnets.default.ids
}

resource "aws_rds_cluster" "pg" {
  cluster_identifier        = "${var.project}-pg"
  engine                    = "postgres"
  engine_version            = var.pg_version
  db_cluster_instance_class = var.pg_cluster_instance_class
  # Multi-AZ DB clusters require provisioned storage; gp3 at 100 GiB is the
  # cheapest valid combo. iops must be OMITTED below 400 GiB (3000 baseline
  # applies implicitly) — specifying it fails with InvalidParameterCombination.
  storage_type            = "gp3"
  allocated_storage       = var.pg_allocated_storage_gb
  storage_encrypted       = true
  database_name           = var.pg_database
  master_username         = var.pg_username
  master_password         = var.pg_password
  db_subnet_group_name    = aws_db_subnet_group.pg.name
  vpc_security_group_ids  = [aws_security_group.rds.id]
  skip_final_snapshot     = true # ponytail: fine for a demo; flip to false in prod.
  deletion_protection     = false
  backup_retention_period = 1 # ponytail: 1 day is enough for the demo.
  apply_immediately       = true
}
