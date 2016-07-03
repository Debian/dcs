Install:

    go get github.com/stapelberg/godebiancontrol

…and then use it in your code:

```go
package main

import (
    "github.com/stapelberg/godebiancontrol"
    "os"
    "log"
)
    
func main() {
    file, err := os.Open("/var/lib/apt/lists/http.debian.net_debian_dists_testing_main_binary-amd64_Packages")
    if err != nil {
        log.Fatal(err)
    }
    defer file.Close()
        
    p, err := godebiancontrol.Parse(file)
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("The first package in the list is %s\n", p[0]["Package"])
}
```

Find the documentation at:

http://go.pkgdoc.org/github.com/stapelberg/godebiancontrol
