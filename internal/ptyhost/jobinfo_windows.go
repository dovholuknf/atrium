//go:build windows

package ptyhost

import "github.com/dovholuknf/atrium/internal/base/detach"

// inJob says whether this process sits in a job object that would end or limit it, and describes the job. A job that
// only allows breakaway ends nothing, so it is not what `probe` answers as in_job (design G5). A host in a job with
// KILL_ON_JOB_CLOSE or a process or memory limit is there because the job REFUSED breakaway, and ending that job ends
// the host and every runner with it.
func inJob() (bool, string) {
	_, binds, detail := detach.SelfJob()
	return binds, detail
}
