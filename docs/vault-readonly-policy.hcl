# Least privilege for the Vault source. Read and list only — expiry-radar never
# needs update, create or delete anywhere.
#
# Note what is NOT here: sys/leases/lookup. Enumerating dynamic leases is a PUT
# and needs `update` plus `sudo`, which would break the read-only promise this
# tool is sold on. A PKI mount answers the same question with reads.

path "auth/token/lookup-self" {
  capabilities = ["read"]
}

path "pki/certs" {
  capabilities = ["list"]
}

path "pki/cert/*" {
  capabilities = ["read"]
}

# Repeat the two blocks above per additional PKI mount, e.g. pki_int.
