package cli

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// runBlockTries is how many blocks a plain `atrium run` tries, a hundred ports apart, before it gives up.
const runBlockTries = 10

// runBlock is the ports a plain `atrium run` binds: the board, the link one above, the room's board three above and
// its agents one below, the scheme `isolatedRoomLaunch` follows.
type runBlock struct{ board, link, human, agent int }

func blockAt(board int) runBlock {
	return runBlock{board: board, link: board + 1, human: board + 3, agent: board - 1}
}

// portProbes asks the machine: is a loopback port free, and does a board on it answer as atrium.
type portProbes struct {
	free   func(port int) bool
	atrium func(port int) bool
}

// pickRunBlock is the block to bind, the recorded one first so a joined room's link still matches. running is true
// when the recorded board is this hub already up. roomUp leaves the room's ports out, since that room keeps its own.
// note is the one line for a move, "" when the first block is free.
func pickRunBlock(recorded runBlock, roomUp bool, p portProbes) (blk runBlock, running bool, note string, err error) {
	if recorded.board > 0 && !p.free(recorded.board) && p.atrium(recorded.board) {
		return recorded, true, "", nil
	}
	def := blockAt(portNumber(defaultBoardAddr()))
	var tries []runBlock
	if recorded.board > 0 && recorded != def {
		tries = append(tries, recorded)
	}
	for i := 0; i < runBlockTries; i++ {
		tries = append(tries, blockAt(def.board+100*i))
	}
	held := 0
	for _, b := range tries {
		ports := []int{b.board, b.link}
		if !roomUp {
			ports = append(ports, b.human, b.agent)
		}
		taken := 0
		for _, port := range ports {
			if !p.free(port) {
				taken = port
				break
			}
		}
		if taken == 0 {
			if held == 0 {
				return b, false, "", nil
			}
			return b, false, movedNote(held, tries[0], p, b, roomUp), nil
		}
		if held == 0 {
			held = taken
		}
	}
	return runBlock{}, false, "", fmt.Errorf("port %d is in use, and so is a port in each of the next %d blocks. "+
		"name free ones with --addr, --link, --http and --agent", held, runBlockTries-1)
}

func movedNote(held int, first runBlock, p portProbes, b runBlock, roomUp bool) string {
	who := "another program"
	if held == first.board && p.atrium(held) {
		who = "another atrium, perhaps another account's"
	}
	moved := fmt.Sprintf("board %d and link %d", b.board, b.link)
	if !roomUp {
		moved = fmt.Sprintf("board %d, link %d, room board %d and agents %d", b.board, b.link, b.human, b.agent)
	}
	return fmt.Sprintf("port %d is in use by %s, so this atrium uses %s", held, who, moved)
}

func portNumber(addr string) int {
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(p)
	return n
}

// recordedBlock is the block hub-addr.json names, the zero block when there is none.
func recordedBlock(dir string) runBlock {
	a := readHubAddrs(dir)
	b := portNumber(a.Board)
	if b <= 1 {
		return runBlock{}
	}
	blk := blockAt(b)
	if l := portNumber(a.Link); l > 0 {
		blk.link = l
	}
	return blk
}

var machinePorts = portProbes{free: loopbackFree, atrium: boardIsAtrium}

func loopbackFree(port int) bool {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

func boardIsAtrium(port int) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) + "/")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	return strings.Contains(string(body), "<title>atrium</title>")
}

func anyChanged(cmd *cobra.Command, names ...string) bool {
	for _, n := range names {
		if cmd.Flags().Changed(n) {
			return true
		}
	}
	return false
}
