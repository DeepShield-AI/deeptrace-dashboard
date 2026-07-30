package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"deeptrace-backend/clickhouse"
	"deeptrace-backend/query/dimension"
)

