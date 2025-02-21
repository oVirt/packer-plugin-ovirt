# oVirt packer.io builder

This builder plugin extends [packer.io](https://packer.io) to support building
images for [oVirt](https://www.ovirt.org).

Based on:

* [cwilloughby-bw/packer-builder-ovirt](https://github.com/cwilloughby-bw/packer-builder-ovirt)
* [ganto/packer-builder-ovirt](https://github.com/ganto/packer-builder-ovirt)

## Development

### Prerequisites

To compile this plugin you must have a working Go compiler setup. Follow the
[official instructions](https://golang.org/doc/install) or use your local
package manager to install Go on your system.

### Compile the plugin

```shell
go build
```

If the build was successful, you should now have the `builder-ovirt`.
