#!/bin/sh
# Initialize the demo key once and preserve its bytes across concurrent starts.
set -eu
set -o pipefail
umask 077

key=/secrets/master.key
temporary=
trap '[ -z "$temporary" ] || rm -f "$temporary"' EXIT HUP INT TERM

[ ! -L "$key" ] || { echo 'keygen: symlink key refused' >&2; exit 1; }
if [ ! -e "$key" ]; then
    temporary=$(mktemp /secrets/.master.key.XXXXXX)
    head -c 32 /dev/urandom | base64 > "$temporary"
    chmod 0400 "$temporary"
    chown 65532:65532 "$temporary"
    # A hard link publishes a complete file without replacing a concurrent winner.
    if ! ln "$temporary" "$key" 2>/dev/null; then
        [ -f "$key" ] || { echo 'keygen: key publication failed' >&2; exit 1; }
    fi
fi

[ -f "$key" ] && [ ! -L "$key" ] || { echo 'keygen: regular key file required' >&2; exit 1; }
decoded_size=$(base64 -d "$key" | wc -c)
[ "$decoded_size" -eq 32 ] || { echo 'keygen: invalid existing key; refusing replacement' >&2; exit 1; }
chown 65532:65532 "$key" /secrets
chmod 0400 "$key"
chmod 0700 /secrets
