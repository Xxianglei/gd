/**
 * Copyright 2020 gd Author. All rights reserved.
 * Author: Xxianglei
 */

package gl

import (
	"github.com/v2pro/plz/gls"
	"strconv"
)

func getGoId() (string, bool) {
	return strconv.FormatInt(gls.GoID(), 10), true
}
