#!/usr/bin/env bash
# The related public repos cloned beside this one (README), for a Claude
# cloud session or a fresh checkout: no credentials needed, all are public.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
top=$(dirname "$here")
clone() { # repo [branch]
	if [ -d "$top/$1/.git" ]; then
		git -C "$top/$1" pull -q --ff-only
	else
		git clone -q --depth 50 ${2:+--branch "$2"} "https://github.com/i41-eu/$1.git" "$top/$1"
	fi
}
clone i41eu-assets
clone q41eu-raw
clone i41eu-embody
clone q41eu-pet
clone q41eu-focus
clone q41eu-dev
clone i41eu-manager
clone i41eu-web
clone i41eu-arch
go -C "$top/i41eu-assets" build ./... # robot3d builds: the model's source is usable
echo "s3d setup: ready ($(go version | cut -d' ' -f3))"
