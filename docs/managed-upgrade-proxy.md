# Local Nginx managed-upgrade transport

The native paired test first failed while waiting for initial Agent reports; builds, login and managed Dashboard restart passed on both architectures. The earlier proxy fixture omitted explicit underscore-header support. Legacy authentication uses the gRPC metadata key client_secret; preserve it when forwarding to Dashboard. Use the following relevant directives in the trusted local TLS gRPC virtual host (replace address, certificates and port as appropriate):

    server {
        listen 443 ssl http2;
        server_name agent.example.invalid;
        ssl_certificate /etc/nginx/certs/agent.pem;
        ssl_certificate_key /etc/nginx/certs/agent.key;
        underscores_in_headers on;
        location / {
            grpc_read_timeout 180s;
            grpc_send_timeout 180s;
            grpc_pass grpc://127.0.0.1:5555;
        }
    }

For a separate HTTPS Dashboard browser virtual host, preserve Host, set X-Forwarded-Proto to https, and set client_max_body_size to 100m for uploads. Never log client_secret, cookies or request bodies. Do not expose the local plaintext backend to untrusted networks. The paired test now enables underscore headers and emits only bounded non-secret status diagnostics on failure; the exact cause of the original timeout is not yet conclusively proven. The next CI must still demonstrate all three Agent reports, successful canary and wave, rollback and replay rejection. Added parallel full Go regression, dependency integrity and reachable vulnerability/frontend scans; no production release occurs.
