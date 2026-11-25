## Steps to generate certificates in terminal for mutual TLS:

### Generate CA certificate:
```
openssl genrsa -out ca.key 2048
openssl req -new -x509 -days 9999 -key ca.key -subj "/C=CN/ST=GD/L=SZ/O=GitLab, Inc./CN=localhost" -out ca.crt
```

### Generate client certificate and sign using above generated CA certificate:
```
echo "subjectAltName = DNS:localhost,IP:127.0.0.1" > test.txt

openssl req -newkey rsa:2048 -nodes -keyout client.key -subj "/C=CN/ST=GD/L=SZ/O=GitLab, Inc./CN=localhost" -out client.csr
openssl x509 -req -extfile test.txt -days 365 -in client.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out client.crt
```