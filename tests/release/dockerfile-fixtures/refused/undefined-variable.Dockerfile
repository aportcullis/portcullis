# A variable that no ARG or ENV defines expands to nothing.
FROM busybox:1.37
ENV TOOL_PATH=$UNDEFINED_TOOL_ROOT/bin
