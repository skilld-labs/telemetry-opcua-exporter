# telemetry_opcua_exporter

## Server Configuration 

info to set in execution flag : 
```go
bindAddress := flag.String("bindAddress", ":4242", "Address to listen on for web interface.")
configFile = flag.String("configFile", "opcua.yaml", "Path to configuration file.")
endpoint := flag.String("endpoint", "opc.tcp://opcua.demo-this.com:51210/UA/SampleServer", "OPC UA Endpoint URL")
auth := flag.String("auth-mode", "UserName", "Authentication Mode: one of Anonymous, UserName, Certificate")
verbosity := flag.String("verbosity", "info", "Log verbosity (debug/info/warn/error/fatal)")
```
To choose policy and mode :
```go
policy := flag.String("sec-policy", "None", "Security Policy URL or one of None, Basic128Rsa15, Basic256, Basic256Sha256")
mode := flag.String("sec-mode", "auto", "Security Mode: one of None, Sign, SignAndEncrypt")
```
"auto" mode will find the highest security level according to selected endpoint
beware that both policy and mode cannot be set to None 

If auth is set to "Certificate" are mandatory :
```go
certfile := flag.String("cert", "cert.crt", "Path to certificate file")
keyfile := flag.String("key", "cert.key", "Path to PEM Private Key file")
```

If auth is set to "UserName" are mandatory :
```go
username := flag.String("user", "admin", "Username to use in auth-mode UserName")
password := flag.String("password", "admin", "Password to use in auth-mode UserName")
```

## Metrics Configuration 

metrics configuration should be through yaml file following this pattern :

```yaml 
metrics:
  - name: Temperature   # MANDATORY
    help: get metrics for machine temperature # MANDATORY
    nodeid: ns=2;i=10853 # MANDATORY and UNIQUE for each metric
    labels: # if metrics share the same name they can be distinguis by labels
      site: MLK
    type: gauge # MANDATORY metric type can be counter, gauge, Float, Double
  - name: Temperature
    help: get metrics for machine temperature
    nodeid: ns=2;i=10852
    labels: 
      site: Paris
    type: gauge
```


## Https Routes 
### show current metrics
```
curl 127.0.0.1:4242/metrics
```
### show current config
```
curl 127.0.0.1:4242/config
```
### reload config from opcua.yaml file 
```
curl 127.0.0.1:4242/reload/config
```
### change config from body's request data
```
curl --request POST \      
  --url 127.0.1:4242/config/update \
  --header 'content-type: x-yaml' \
  --data "metrics:
  - name: plop
    help: plop
    nodeid: ns=2;i=10853
    labels: {}
    type: gauge"
```
Please take note that it's also rewrite opcua.yaml with the input file 
