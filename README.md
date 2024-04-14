# RouterOS Route Management Tool

This tool allows for management of routing entries in MikroTik RouterOS based on domain name resolutions. It supports listing existing routes, adding new routes, and updating or removing existing routes, making it suitable for dynamic DNS or IP address management tasks.

## Features

- **List Routes**: List existing routes filtered by a specified gateway and domain (as a comment).
- **Update Routes**: Automatically re-resolve domain names and update routing entries accordingly.
- **Add Routes**: Add new routes for resolved IP addresses with domain names as comments.
- **Dry Run**: Simulate management actions without making changes to the RouterOS configuration.

## Requirements

- Go programming environment
- RouterOS API library for Go (`github.com/swoga/go-routeros`)

## Installation

1. Ensure Go is installed on your system.
2. Install with Go way:

   ```
   go install github.com/skrashevich/goaround-block-mikrotik@latest
   ```

## Usage

The tool can be run from the command line with various flags to control its operation:

```
./goaround-block-mikrotik -address <RouterOS IP>:<port> -username <username> -password <password> [options]
```

### Command Line Options

- `-domain <domain>`: Specify the domain to resolve and manage routes for.
- `-address <address>`: Set the RouterOS device's address with port (`IP:Port` format; default port is 8728 if unspecified).
- `-username <username>`: Username for RouterOS authentication (default is "admin").
- `-password <password>`: Password for RouterOS authentication.
- `-gateway <gateway>`: Specify the gateway IP address for the new routes.
- `-list`: Enable listing of routes that match the specified domain and gateway.
- `-update`: Update existing routes by re-resolving domain names.
- `-dry`: Simulate actions without applying changes to RouterOS.

### Examples

**List Current Routes**:

```
./goaround-block-mikrotik  -address 192.168.88.1:8728 -username admin -password yourpassword -gateway 192.168.88.1 -domain example.com -list
```

**Update All Routes**:

```
./goaround-block-mikrotik  -address 192.168.88.1:8728 -username admin -password yourpassword -gateway 192.168.88.1 -update
```

**Dry Run of Update**:

```
./goaround-block-mikrotik  -address 192.168.88.1:8728 -username admin -password yourpassword -gateway 192.168.88.1 -update -dry
```

## Contributing

Contributions to improve the tool or extend its capabilities are welcome. Please submit pull requests or report issues through the project's GitHub page.

## License

This project is licensed under the MIT License. See the LICENSE file for more details.
