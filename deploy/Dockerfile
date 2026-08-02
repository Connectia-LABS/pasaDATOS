FROM scratch

WORKDIR /
COPY bin/pasadatos-server-linux-amd64 /pasadatos
VOLUME ["/data"]
EXPOSE 8088

ENTRYPOINT ["/pasadatos", "--server"]
