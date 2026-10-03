#!/bin/sh
# Run only in a disposable container with an empty /secrets tmpfs.
set -eu
[ ! -e /secrets/master.key ] || { echo 'test requires an empty secrets fixture' >&2; exit 1; }
adduser -D -u 65532 runtime

sh /keygen/generate-master-key.sh & first=$!
sh /keygen/generate-master-key.sh & second=$!
wait "$first"
wait "$second"
test "$(stat -c %u:%g:%a /secrets/master.key)" = 65532:65532:400
test "$(stat -c %u:%g:%a /secrets)" = 65532:65532:700
su -s /bin/sh runtime -c 'test -r /secrets/master.key && test "$(base64 -d /secrets/master.key | wc -c)" -eq 32'
if su -s /bin/sh nobody -c 'test -r /secrets/master.key'; then
    echo 'unrelated user can read master key' >&2; exit 1
fi
before=$(sha256sum /secrets/master.key)
sh /keygen/generate-master-key.sh
test "$before" = "$(sha256sum /secrets/master.key)"

printf 'invalid-test-fixture' > /secrets/master.key
before=$(sha256sum /secrets/master.key)
if sh /keygen/generate-master-key.sh; then
    echo 'corrupt existing key accepted' >&2; exit 1
fi
test "$before" = "$(sha256sum /secrets/master.key)"
rm /secrets/master.key
touch /secrets/master.key
if sh /keygen/generate-master-key.sh; then
    echo 'empty existing key replaced' >&2; exit 1
fi
test ! -s /secrets/master.key
rm /secrets/master.key
printf 'symlink-test-fixture' > /secrets/symlink-target
ln -s /secrets/symlink-target /secrets/master.key
before=$(sha256sum /secrets/symlink-target)
if sh /keygen/generate-master-key.sh; then
    echo 'symlink key accepted' >&2; exit 1
fi
test "$before" = "$(sha256sum /secrets/symlink-target)"
echo 'Key generation scenarios passed.'
