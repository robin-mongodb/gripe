provider "aws" {
  region = var.aws_region
  # ponytail: reads ~/.aws/credentials by default. Set AWS_PROFILE env var to switch profile.
  default_tags {
    tags = {
      Project     = var.project
      ManagedBy   = "terraform"
      Environment = "demo"
    }
  }
}
