# vhost

Simple couchdb database config based vhost handling

## Getting started

    handler = vhost.Middleware(gdb)

## Database

Uses the `_admin` database for vhost configurations.

Every vhost needs a `goydb.vhost:` prefix. A vhost can have proxy
configurations for `reverse` and `db` (database) proxing.

The vhost will be reacting to all specified `domains`.

Attachments can be used to serve static content at the root
of the vhost.

    {
        "_id": "goydb.vhost:example",
        "_attachments": {
            "app.zip": {
                "content_type": "application/x-zip-compressed",
                "revpos": 0,
                "digest": "21d318cbe5645a50257d1062d9dffb64",
                "length": 712001,
                "stub": true
            }
        },
        "_rev": "0-db20aa37f912a2d765a6197b0246b1e9",
        "domains": [
            "app.example.com",
            "app.example.internal"
        ],
        "proxy": {
            "/_session": {
                "target": "https://sessions.example.com/",
                "type": "reverse"
            },
            "/other": {
                "stripPrefix": true,
                "target": "https://other.example.com/dms/",
                "type": "reverse"
            },
            "/db": {
                "target": "appdb",
                "type": "db"
            }
        },
        "static": "app.zip"
    }
