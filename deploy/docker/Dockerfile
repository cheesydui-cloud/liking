# liking-managed-dockerfile
FROM scratch
COPY ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY liking-server /usr/local/bin/liking-server
ENV SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt
EXPOSE 8899
STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/local/bin/liking-server"]
CMD ["--addr", "0.0.0.0:8899", "--db", "/var/lib/liking/panel.db"]
HEALTHCHECK --interval=30s --timeout=3s --start-period=20s --retries=3 CMD ["/usr/local/bin/liking-server", "--health", "http://127.0.0.1:8899/healthz"]
