FROM elementsproject/lightningd:v23.11.2-amd64

# Install optional dependencies for lspd_cln_plugin to work
RUN apt update && apt install -y \
  libc-dev-bin
