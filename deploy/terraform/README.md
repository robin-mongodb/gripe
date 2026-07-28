# deploy/terraform — one-apply Gripe demo infra

**What it provisions:** EC2 (t3.small) + RDS PostgreSQL (db.t4g.micro) + three SQS queues + IAM. Atlas is out of scope — you own that cluster; pass its URI via `app_env.MONGO_URI` in `terraform.tfvars`.

**State is local.** `terraform.tfstate` sits in this directory (gitignored). Fine for a single-operator demo; graduate to an S3 backend if two people ever run apply.

## Prereqs (one-time)

1. AWS access key + secret in `~/.aws/credentials`. `aws sts get-caller-identity` should work.
2. Terraform ≥ 1.6 installed (`brew install terraform` or the tfenv approach).
3. An SSH keypair on your laptop. Make one if needed: `ssh-keygen -t ed25519`.

## Deploy

```bash
cd deploy/terraform
cp terraform.tfvars.example terraform.tfvars
# edit terraform.tfvars — paste ssh_pubkey, allowed_ssh_cidr (curl ifconfig.me), pg_password, MONGO_URI

terraform init
terraform plan   # eyeball what's about to happen
terraform apply  # ~5 min for RDS to come up
```

Outputs give you the public IP, ssh command, RDS endpoint, SQS URLs.

## First boot

`user-data.sh` runs on the EC2 automatically:
1. Installs Docker + Compose plugin.
2. Clones the repo to `/opt/gripe`.
3. Writes `.env` (RDS DSN, SQS URLs, MONGO_URI from tfvars, etc.).
4. Runs `docker compose up -d --build`.

First boot takes ~2 min after `apply` completes. Then `http://<ec2-ip>/` renders the Next.js pages; `http://<ec2-ip>/v1/healthz` returns the JSON.

## Redeploy on code change

SSH into the box and run the helper user-data installed:

```bash
ssh ec2-user@<ip>
sudo gripe-redeploy   # git pull + docker compose up -d --build
```

That's the entire loop. No CI needed for a two-week demo.

## Change tfvars

Edit `terraform.tfvars`, run `terraform apply`. Note the `ignore_changes = [user_data]` on the EC2 — updating env vars alone won't re-bootstrap the box. To force a re-bootstrap: `terraform taint aws_instance.app && terraform apply` (**destroys the local Docker state**).

## Destroy when idle

```bash
terraform destroy
```

Kills everything, stops all costs. Re-run `terraform apply` to bring it back.

## Rough cost (running 24×7)

| Resource                     | 2wk cost |
| ---------------------------- | -------- |
| t3.small                     | ~$8      |
| db.t4g.micro + 20 GB gp3     | ~$18     |
| SQS                          | free tier |
| Egress                       | pennies  |
| **Total**                    | **~$26** |

Destroy overnight to roughly halve that. RDS storage keeps charging while snapshotted; leave `skip_final_snapshot = true` (default) to avoid post-destroy leftovers.
