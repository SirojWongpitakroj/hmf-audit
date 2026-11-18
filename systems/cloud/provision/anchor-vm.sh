#!/usr/bin/env bash
# Provision the blockchain anchor VM (e2-medium, x86-64). Run it ON that VM.
#
#   anchor-vm.sh               install Node.js, start a Hardhat node as a service, deploy EpochAnchor
#   anchor-vm.sh --deploy-only redeploy EpochAnchor (a Hardhat node keeps its state in memory,
#                              so the contract is gone after the VM restarts)
#
# The node is the same Hardhat the baselines use (chain id 31337, from the
# EVSEB project, v2.29). Nothing in the timed benchmark paths uses it: those
# keep the in-memory chain, so "blockchain network not in primary timers"
# still holds. Hardhat's development accounts are public, so this node must
# only ever be reachable from the runner (firewall port 8545 to 10.148.0.8).
. "$(dirname "${BASH_SOURCE[0]}")/common.sh"

: "${CLOUD_ANCHOR_PORT:=8545}"
EVBES_DIR="$SYSTEMS_DIR/EVBES"
DHT_DIR="$SYSTEMS_DIR/dht-backed"

deploy_epoch_anchor() {
    note "deploying EpochAnchor (dht-backed)"
    (cd "$DHT_DIR" && npm ci --no-audit --no-fund && npx hardhat compile \
        && DHT_ETH_RPC_URL="http://127.0.0.1:$CLOUD_ANCHOR_PORT" npx hardhat run scripts/deploy.js --network localhost) \
        | tee "$CLOUD_DIR/anchor-deploy.out"
    note "contract address saved in $CLOUD_DIR/anchor-deploy.out (DHT_CONTRACT_ADDRESS=...)"
}

if [[ "${1:-}" == --deploy-only ]]; then
    deploy_epoch_anchor
    exit 0
fi

if ! command -v node >/dev/null 2>&1 || (( $(node -p 'process.versions.node.split(".")[0]') < 20 )); then
    note "installing Node.js 20"
    curl -fsSL https://deb.nodesource.com/setup_20.x | sudo -E bash -
    sudo apt-get install -y nodejs
fi
note "installing the Hardhat project dependencies"
(cd "$EVBES_DIR" && npm ci --no-audit --no-fund)

sudo tee /etc/systemd/system/fair-anchor.service >/dev/null <<EOF
[Unit]
Description=Hardhat node (blockchain anchor)
After=network-online.target
Wants=network-online.target

[Service]
User=$USER
WorkingDirectory=$EVBES_DIR
ExecStart=$(command -v npx) hardhat node --hostname 0.0.0.0 --port $CLOUD_ANCHOR_PORT
Restart=on-failure

[Install]
WantedBy=multi-user.target
EOF
sudo systemctl daemon-reload
sudo systemctl enable --now fair-anchor

note "waiting for the node"
for ((i = 0; i < 60; i++)); do
    curl -fsS -m 3 -X POST -H 'Content-Type: application/json' \
        --data '{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}' \
        "http://127.0.0.1:$CLOUD_ANCHOR_PORT" 2>/dev/null | grep -q '"0x7a69"' && break
    sleep 2
done
curl -fsS -m 3 -X POST -H 'Content-Type: application/json' \
    --data '{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}' \
    "http://127.0.0.1:$CLOUD_ANCHOR_PORT" | grep -q '"0x7a69"' || die "the Hardhat node did not start (journalctl -u fair-anchor)"
deploy_epoch_anchor
note "done: anchor node on $(internal_ip):$CLOUD_ANCHOR_PORT (chain id 31337)."
