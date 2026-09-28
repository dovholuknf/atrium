package daemon

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/openziti/sdk-golang/ziti"
	"github.com/openziti/sdk-golang/ziti/enroll"
)

// enrollWithSDK enrolls a one-time-token JWT in this process, with the ziti SDK
// atrium already embeds, and writes the identity to outPath.
//
// THE FALLBACK FOR A MACHINE WITH NO ziti CLI, which is most machines a room is
// provisioned onto. The CLI stays first when it is there, because it is what
// the board's enroll has always run and what an operator can run by hand to
// check. This does the same enrolment the CLI's `ziti edge enroll` does: a new
// EC key made here, a certificate from the controller, and a config file that
// holds both. The key never leaves this machine.
func enrollWithSDK(token, outPath string) error {
	claims, tok, err := enroll.ParseToken(token)
	if err != nil {
		return fmt.Errorf("that is not an enrollment token: %w", err)
	}
	var alg ziti.KeyAlgVar
	if err := alg.Set("EC"); err != nil {
		return err
	}
	cfg, err := enroll.Enroll(enroll.EnrollmentFlags{
		Token: claims, JwtToken: tok, JwtString: token, KeyAlg: alg,
	})
	if err != nil {
		return fmt.Errorf("the controller would not enroll it: %w", err)
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(outPath, raw, 0o600)
}
