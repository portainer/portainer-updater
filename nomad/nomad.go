package nomad

const (
	// NomadAddrEnvVarName represent the name of environment variable of the Nomad addr
	NomadAddrEnvVarName = "NOMAD_ADDR"
	// NomadNamespaceEnvVarName represent the name of environment variable of the Nomad namespace
	NomadNamespaceEnvVarName = "NOMAD_NAMESPACE"
	// NomadCACertEnvVarName represent the name of environment variable of the Nomad ca certificate
	NomadCACertEnvVarName = "NOMAD_CACERT"
	// NomadClientCertEnvVarName represent the name of environment variable of the Nomad client certificate
	NomadClientCertEnvVarName = "NOMAD_CLIENT_CERT"
	// NomadClientKeyEnvVarName represent the name of environment variable of the Nomad client key
	NomadClientKeyEnvVarName = "NOMAD_CLIENT_KEY"
	// NomadCACertContentEnvVarName represent the name of environment variable of the Nomad ca certificate content
	NomadCACertContentEnvVarName = "NOMAD_CACERT_CONTENT"
	// NomadClientCertContentEnvVarName represent the name of environment variable of the Nomad client certificate content
	NomadClientCertContentEnvVarName = "NOMAD_CLIENT_CERT_CONTENT"
	// NomadClientKeyContentEnvVarName represent the name of environment variable of the Nomad client key content
	NomadClientKeyContentEnvVarName = "NOMAD_CLIENT_KEY_CONTENT"
	// NomadTLSCACertPath is the default path to the Nomad TLS CA certificate file.
	NomadTLSCACertPath = "nomad-ca.pem"
	// NomadTLSCertPath is the default path to the Nomad TLS certificate file.
	NomadTLSCertPath = "nomad-cert.pem"
	// NomadTLSKeyPath is the default path to the Nomad TLS key file.
	NomadTLSKeyPath = "nomad-key.pem"
	// TLSCertPath is the default path to the TLS certificate file.
)
