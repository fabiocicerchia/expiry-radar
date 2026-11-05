def lines(p):
    return open(p).read().split("\n")

def rng(ls, a, b):  # 1-indexed inclusive
    return ls[a - 1:b]

c = "internal/config/config.go"
L = lines(c)
open("internal/config/file.go", "w").write("\n".join(
    ["package config", "", "import (",
     '\t"github.com/fabiocicerchia/expiry-radar/internal/rank"',
     '\t"github.com/fabiocicerchia/expiry-radar/internal/source"', ")", ""]
    + rng(L, 20, 295)) + "\n")
open("internal/config/sources.go", "w").write("\n".join(
    ["package config", "", "import (", '\t"time"', "",
     '\t"github.com/fabiocicerchia/expiry-radar/internal/source"', ")", ""]
    + rng(L, 550, 725)) + "\n")
open(c, "w").write("\n".join(L[:19] + L[296:549]).rstrip("\n") + "\n")

f = "internal/source/cloudflare.go"
L = lines(f)
open("internal/source/cloudflare_api.go", "w").write("\n".join(
    ["package source", "", "import (", '\t"context"', '\t"encoding/json"', '\t"fmt"',
     '\t"net/http"', '\t"strconv"', '\t"strings"', '\t"time"', ")", ""]
    + rng(L, 55, 62) + rng(L, 89, 174) + rng(L, 549, 562)) + "\n")
open(f, "w").write("\n".join(L[:54] + L[62:88] + L[174:548]).rstrip("\n") + "\n")
