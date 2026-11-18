#!/usr/bin/env bash
# Provision the benchmark runner (t2a-standard-4, Arm). Run it ON the runner,
# from a clone of this repository, over SSH.
#
#   git clone https://github.com/SirojWongpitakroj/hmf-audit.git && cd hmf-audit
#   cp <your cloud.env> systems/cloud/cloud.env
#   bash systems/cloud/provision/runner-vm.sh
#
# Installs Git, Go 1.27, Python + matplotlib, the PostgreSQL client and Docker
# (Docker only supplies `cqlsh`, from the Cassandra image), and fetches the
# hmf-audit submodule. The databases are NOT installed here: they live on the
# five database VMs.
. "$(dirname "${BASH_SOURCE[0]}")/common.sh"

GO_VERSION=1.27.0
case "$(uname -m)" in
aarch64 | arm64) GO_ARCH=arm64 ;;
x86_64) GO_ARCH=amd64; echo "  warn: this is not an Arm VM; the cloud plan uses t2a (Arm)" >&2 ;;
*) die "unsupported architecture $(uname -m)" ;;
esac

note "installing packages"
sudo apt-get update -y
sudo apt-get install -y git curl python3 python3-pip python3-venv postgresql-client

if ! command -v go >/dev/null 2>&1 || ! go version | grep -q "go$GO_VERSION"; then
    note "installing Go $GO_VERSION ($GO_ARCH)"
    curl -fsSL -o /tmp/go.tgz "https://go.dev/dl/go${GO_VERSION}.linux-${GO_ARCH}.tar.gz"
    sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf /tmp/go.tgz && rm -f /tmp/go.tgz
    grep -q '/usr/local/go/bin' "$HOME/.profile" || echo 'export PATH=$PATH:/usr/local/go/bin' >>"$HOME/.profile"
fi
export PATH=$PATH:/usr/local/go/bin
go env -w CGO_ENABLED=0

ensure_docker
sudo usermod -aG docker "$USER" || true   # takes effect at the next login

VENV="$SYSTEMS_DIR/../.venv"
[[ -d "$VENV" ]] || python3 -m venv "$VENV"
"$VENV/bin/python" -m pip install --quiet -r "$SYSTEMS_DIR/../requirements.txt"

note "fetching the hmf-audit submodule over HTTPS"
git -C "$SYSTEMS_DIR/.." -c url."https://github.com/".insteadOf=git@github.com: submodule update --init

note "pulling the Cassandra image used for cqlsh"
sudo docker pull "$CASSANDRA_IMAGE" >/dev/null

note "done. Log out and back in (docker group), activate the venv:"
echo "  source $VENV/bin/activate"
echo "then run:  bash systems/cloud/check.sh pre"
