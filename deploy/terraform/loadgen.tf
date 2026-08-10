# Load-generator box for the perf phase (tasks 40-42). Separate machine so k6
# never steals CPU from the system under test; same subnet so the client->api
# hop stays constant and the only variable is the database.
# Off by default — flip enable_loadgen = true in terraform.tfvars for perf runs,
# false again after (it's the whole teardown).

resource "aws_security_group" "loadgen" {
  count       = var.enable_loadgen ? 1 : 0
  name        = "${var.project}-loadgen"
  description = "Gripe load generator: SSH from your IP, all egress."
  vpc_id      = data.aws_vpc.default.id

  ingress {
    description = "SSH"
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = [var.allowed_ssh_cidr]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

# k6 hits the api on 8080 directly — nginx rate-limits /v1/ to 100 r/s, which
# would dominate any perf measurement.
resource "aws_security_group_rule" "api_from_loadgen" {
  count                    = var.enable_loadgen ? 1 : 0
  type                     = "ingress"
  description              = "api port for k6, bypassing nginx"
  from_port                = 8080
  to_port                  = 8080
  protocol                 = "tcp"
  security_group_id        = aws_security_group.ec2.id
  source_security_group_id = aws_security_group.loadgen[0].id
}

resource "aws_instance" "loadgen" {
  count                  = var.enable_loadgen ? 1 : 0
  ami                    = data.aws_ami.al2023.id
  instance_type          = var.loadgen_instance_type
  subnet_id              = data.aws_subnets.default.ids[0]
  key_name               = aws_key_pair.this.key_name
  vpc_security_group_ids = [aws_security_group.loadgen[0].id]

  user_data = <<-EOF
    #!/bin/bash
    set -euxo pipefail
    dnf install -y git
    curl -sL https://github.com/grafana/k6/releases/download/v0.54.0/k6-v0.54.0-linux-amd64.tar.gz \
      | tar xz --strip-components=1 -C /usr/local/bin k6-v0.54.0-linux-amd64/k6
    sudo -u ec2-user git clone "${var.repo_url}" /home/ec2-user/gripe
    echo "loadgen ready: cd gripe/perf && k6 run -e BASE=http://${aws_instance.app.private_ip}:8080/v1 scenarios.js" \
      > /etc/motd
  EOF

  tags = {
    Name = "${var.project}-loadgen"
  }
}

output "loadgen_public_ip" {
  value       = var.enable_loadgen ? aws_instance.loadgen[0].public_ip : null
  description = "SSH here to run k6. Null when enable_loadgen = false."
}
