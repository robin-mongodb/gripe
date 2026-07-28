#!/bin/bash
# EC2 first-boot script. Installs Docker + git, clones the repo, writes .env, brings compose up.
# Runs once on instance creation. Re-runs require: sudo cloud-init clean && sudo reboot.
set -euxo pipefail

# --- packages ---
dnf update -y
dnf install -y docker git
systemctl enable --now docker
usermod -aG docker ec2-user

# Docker Compose v2 plugin
mkdir -p /usr/local/lib/docker/cli-plugins
curl -SL "https://github.com/docker/compose/releases/download/v2.29.7/docker-compose-linux-x86_64" \
  -o /usr/local/lib/docker/cli-plugins/docker-compose
chmod +x /usr/local/lib/docker/cli-plugins/docker-compose

# --- clone the app ---
install -d -o ec2-user -g ec2-user /opt/gripe
sudo -u ec2-user git clone "${repo_url}" /opt/gripe

# --- write .env from Terraform-rendered values ---
cat > /opt/gripe/.env <<'ENVEOF'
${env_file}
ENVEOF
chown ec2-user:ec2-user /opt/gripe/.env
chmod 600 /opt/gripe/.env

# --- boot the stack ---
cd /opt/gripe
sudo -u ec2-user docker compose up -d --build

# --- redeploy helper for humans ---
cat > /usr/local/bin/gripe-redeploy <<'HELPEOF'
#!/bin/bash
set -euxo pipefail
cd /opt/gripe
git pull --ff-only
docker compose up -d --build
HELPEOF
chmod +x /usr/local/bin/gripe-redeploy

echo "gripe: first-boot bootstrap done"
