# A named build context supplies the base image without an ARG in FROM.
FROM base-image
ENTRYPOINT ["sleep", "infinity"]
