# Shell-form ENTRYPOINT does not receive stop signals.
FROM busybox:1.37
ENTRYPOINT sleep infinity
