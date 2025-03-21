# Microcluster REST API

Microcluster offers a default REST API called the Core API for managing a cluster.

The Core API is offered over the local unix socket as well as over the cluster address of initialized clusters.

There are 4 components to the Core API:

* `/core/internal`: The prefix for all internal endpoints used internally by Microcluster.
* `/core/control`: The prefix for all internal endpoints available exclusively over the unix socket.
* `/core/1.0`: The prefix for all stable endpoints that are available for direct external use.
* `/*`: Extensions to the Core API provided by the external project.

Any additions to the internal Microcluster API will happen under the `/core` endpoint path prefix. This path prefix is reserved and should not be used for additionally supplied API resources.

## Additional servers

Microcluster can set up additional servers with `DaemonArgs.Servers`:

```go
dargs := microcluster.DaemonArgs{
    // If specified, a listener will start, using the `server` keypair, and offer all extensions to the CoreAPI as well as `/core/1.0`.
    // Once the daemon is bootstrapped or joins a cluster, this listener will turn off permanently.
    PreInitListenAddress: "10.0.0.100:8000",

    // Set of servers to set up with the Core API.
    Servers: map[string]Server{
        // Unique internal name for the server.
        "extend-core-api": {
            // Don't set up an additional server, instead serve all resources over the Core API.
            CoreAPI: true,

            // Allow resources to be available over the unix socket or PreInitListenAddress before bootstrapping or joining a cluster.
            PreInit: true,

            // Allow resources to be served over the local unix socket.
            ServeUnix: true,

            // Default address to serve the additional listener over.
            //
            // Cannot be used with CoreAPI=true, because the CoreAPI address and port will be used.
            ServerConfig: types.ServerConfig{},


            // DedicatedCertificate sets whether the additional listener should use its own self-signed certificate.
            // If false it tries to use a custom certificate from the daemon's state `/certificates` directory
            // based on the name provided when creating the server.
            // In case there isn't any custom certificate it falls back to the cluster certificate of the Core API.
            //
            // Cannot be used with CoreAPI=true, as the Core API cluster certificate will be used.
            DedicatedCertificate: false,

            // Resources is the list of resources offered by this server.
            Resources: []Resources{...},

            // DrainConnectionsTimeout is the amount of time to allow for all connections to drain when shutting down.
            // If it's 0, the connections are not drained when shutting down.
            DrainConnectionsTimeout: 0,
        },

        "additional-api": {
            CoreAPI: false,
            PreInit: false,
            ServeUnix: false,
            DrainConnectionsTimeout: 0,

            // Start the additional listener on a different port by default.
            ServerConfig: types.ServerConfig{ Address: "10.0.0.100:9000" },

            // A certificate will be written to `{state-dir}/certificates/additional-api.crt` and `{state-dir}/certificates/additional-api.key`.
            DedicatedCertificate: true,

            // Resources to offer.
            Resources: []Resources{...},
        },
    },
}
```
