#!/bin/bash
VERSION=${1:-"v2025.1.7"}

go install github.com/roadrunner-server/velox/v2025/cmd/vx@"$VERSION"
rm velox.toml
wget "https://raw.githubusercontent.com/roadrunner-server/velox/refs/tags/$VERSION/velox.toml"
sed -i 's/level = "info"/level = "debug"/g' velox.toml
UP_TWO_LEVELS=$(realpath ".")
cat <<EOF >> velox.toml

[github.plugins.mesi]
ref = "main"
owner = "crazy-goat"
repository = "go-mesi"
folder = "servers/roadrunner"
replace = "$UP_TWO_LEVELS"
EOF

vx build -c velox.toml -o "$UP_TWO_LEVELS"
rm velox.toml