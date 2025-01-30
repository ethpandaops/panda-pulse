FROM alpine:latest
COPY panda-pulse /usr/local/bin/
ENTRYPOINT ["panda-pulse"] 