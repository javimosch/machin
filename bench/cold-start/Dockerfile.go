# Go: a static (CGO_ENABLED=0) binary also ships FROM scratch — just larger.
FROM scratch
COPY hello-go /hello
EXPOSE 8091
ENTRYPOINT ["/hello"]
