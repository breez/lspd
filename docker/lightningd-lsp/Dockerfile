FROM lightningd

COPY --from=lspd /usr/local/bin/lspd_cln_plugin /usr/local/bin/

# lightningd grpc port
EXPOSE 8888

# cln_plugin grpc port
EXPOSE 12312

CMD [ \
    # "--grpc-port=8888", \
    "--plugin=/usr/local/bin/lspd_cln_plugin", \
    "--lsp-listen=0.0.0.0:12312", \
    "--developer", \
    "--dev-allowdustreserve=true" \
]