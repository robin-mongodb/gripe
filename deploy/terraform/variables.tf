variable "aws_region" {
  type        = string
  default     = "eu-west-1"
  description = "AWS region for all resources."
}

variable "project" {
  type        = string
  default     = "gripe"
  description = "Name prefix for all resources."
}

variable "ssh_pubkey" {
  type        = string
  description = "Contents of your ~/.ssh/id_ed25519.pub (or id_rsa.pub). Used as the EC2 key pair so you can SSH in."
}

variable "allowed_ssh_cidr" {
  type        = string
  description = "Your public IPv4 in CIDR form, e.g. \"203.0.113.5/32\". Restricts SSH to just you."
}

variable "allowed_http_cidr" {
  type        = string
  default     = "0.0.0.0/0"
  description = "Who can hit port 80. Default is the internet; tighten for a private demo."
}

variable "ec2_instance_type" {
  type        = string
  default     = "t3.small"
  description = "EC2 size. t3.small is enough for the demo (~$8/2wk)."
}

variable "pg_instance_class" {
  type        = string
  default     = "db.t4g.micro"
  description = "RDS instance class. db.t4g.micro is Graviton-backed, cheapest that supports PG 16."
}

variable "pg_allocated_storage_gb" {
  type        = number
  default     = 20
  description = "gp3 storage in GB. 20 is the minimum for RDS PG."
}

variable "pg_version" {
  type        = string
  default     = "16.14"
  description = "PostgreSQL engine version. AWS deprecates minors quickly — query with `aws rds describe-db-engine-versions --engine postgres --region <region> --query 'DBEngineVersions[?starts_with(EngineVersion, `16.`)].EngineVersion'` if this errors."
}

variable "pg_database" {
  type        = string
  default     = "gripe"
  description = "Initial database name."
}

variable "pg_username" {
  type        = string
  default     = "gripe_admin"
  description = "Master username for RDS. Not the same as application users you may create later."
}

variable "pg_password" {
  type        = string
  sensitive   = true
  description = "Master password for RDS. Set via terraform.tfvars (which is gitignored)."
}

variable "repo_url" {
  type        = string
  default     = "https://github.com/robin-mongodb/gripe.git"
  description = "Public git URL the EC2 clones on first boot."
}

variable "app_env" {
  type        = map(string)
  default     = {}
  sensitive   = true
  description = "Extra env vars written into /opt/gripe/.env on the EC2. Put MONGO_URI here (Atlas is external — user-owned)."
}
