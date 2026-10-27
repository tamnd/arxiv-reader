#!/bin/sh
# Build the reading site of the corpus tamnd/arxiv and publish it to Cloudflare Pages at https://arxiv.tamnd.com.
#
# Run it from cron on the server of the pipeline, for example every 10 minutes: */10 * * * * $HOME/arxiv-site/deploy-site.sh >> $HOME/arxiv-site/deploy.log 2>&1. The server needs git, Go, Node and flock. The corpus clone shares the objects of the checkout of the pipeline ($AX_CORPUS_REPO) and fetches main from GitHub ($AX_CORPUS_URL), because the pipeline merges its pull requests on GitHub and the main of its checkout is old. The fetch is small, because most objects are in the checkout. The script also fetches main of tamnd/arxiv-reader. It does nothing when the two commits are the same as at the last deploy. Use -f to deploy again.
#
# The file $AX_SITE/cloudflare.env must set CLOUDFLARE_API_TOKEN and CLOUDFLARE_ACCOUNT_ID. Keep it at mode 600.
set -eu

dir=${AX_SITE:-$HOME/arxiv-site}
repo=${AX_CORPUS_REPO:-$HOME/arxiv-pipeline/arxiv}
remote=${AX_CORPUS_URL:-https://github.com/tamnd/arxiv.git}
project=${AX_PAGES_PROJECT:-arxiv}
url=${AX_SITE_URL:-https://arxiv.tamnd.com}
force=${1:-}

mkdir -p "$dir"
exec 9>"$dir/lock"
flock -n 9 || { echo "another deploy is running"; exit 0; }

set -a
. "$dir/cloudflare.env"
set +a

# The corpus: a clone that shares the objects of the checkout of the pipeline, with only the directories that the site reads.
if [ ! -d "$dir/corpus/.git" ]; then
	git clone -q --shared --no-checkout "$repo" "$dir/corpus"
	git -C "$dir/corpus" sparse-checkout set metadata analysis/objects analysis/edges reports
fi
git -C "$dir/corpus" remote set-url origin "$remote"
git -C "$dir/corpus" fetch -q origin main
git -C "$dir/corpus" reset -q --hard FETCH_HEAD
corpus=$(git -C "$dir/corpus" rev-parse HEAD)

if [ -d "$dir/toolchain/.git" ]; then
	git -C "$dir/toolchain" fetch -q --depth 1 origin main
	git -C "$dir/toolchain" reset -q --hard FETCH_HEAD
else
	git clone -q --depth 1 https://github.com/tamnd/arxiv-reader.git "$dir/toolchain"
fi
toolchain=$(git -C "$dir/toolchain" rev-parse HEAD)

state="$corpus $toolchain"
if [ "$force" != "-f" ] && [ -f "$dir/deployed" ] && [ "$(cat "$dir/deployed")" = "$state" ]; then
	exit 0
fi

echo "$(date -u +%FT%TZ) deploy arxiv $corpus, arxiv-reader $toolchain"
(cd "$dir/toolchain" && go build -trimpath -o "$dir/ax" ./cmd/ax)
rm -rf "$dir/site"
(cd "$dir/corpus" && ARXIV_CORPUS="$dir/corpus" "$dir/ax" site build -out "$dir/site" -url "$url")
npx --yes wrangler@4 pages deploy "$dir/site" --project-name "$project" --branch main --commit-hash "$corpus" --commit-message "arxiv $corpus" --commit-dirty=true
echo "$state" >"$dir/deployed"
