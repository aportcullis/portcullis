# An image ARG without a default is invalid when no build argument is supplied.
ARG BASE_IMAGE
FROM ${BASE_IMAGE}
