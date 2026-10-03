# An image ARG with a default stays valid without build arguments.
ARG BASE_IMAGE=busybox:1.37
FROM ${BASE_IMAGE}
