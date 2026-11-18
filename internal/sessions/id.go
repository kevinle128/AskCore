package sessions

import (
	"math/rand/v2"
	"sync"

	"github.com/bwmarrin/snowflake"
)

// snowflakeNodes is the number of node numbers that the package defaults
// (NodeBits of 10) allow.
const snowflakeNodes = 1 << 10

var (
	idOnce sync.Once
	idNode *snowflake.Node
)

// NewID returns a new session id: the decimal text of a snowflake id. It has
// 19 digits for ids made from about 2017 to 2080, so the string order of two
// ids is their time order and callers compare ids as strings, never as numbers.
// One node per process, with a random node number, keeps two processes from
// making the same id.
func NewID() string {
	idOnce.Do(func() {
		node, err := snowflake.NewNode(int64(rand.IntN(snowflakeNodes)))
		if err != nil {
			// A node number in range cannot fail; a failure is a broken library.
			panic("sessions: snowflake node: " + err.Error())
		}
		idNode = node
	})
	return idNode.Generate().String()
}
