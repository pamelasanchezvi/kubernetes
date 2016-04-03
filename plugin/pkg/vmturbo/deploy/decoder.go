package deploy

import (
	"encoding/xml"
	"fmt"
	"strings"
)

func GetPodReservationDestination(content string) (string, error) {
	// This is a temp solution. delete the encoding header.
	validStartIndex := strings.Index(content, ">")
	validContent := content[validStartIndex:]

	se := &ServiceEntities{}
	err := xml.Unmarshal([]byte(validContent), se)
	if err != nil {
		return "", fmt.Errorf("Error decoding content: %s", err)
	}
	if se == nil {
		return "", fmt.Errorf("Error decoding content. Result is null.")
	} else {
		if len(se.ActionItems) < 1 {
			return "", fmt.Errorf("Error decoding content. No ActionItem.")
		} else {
			if se.ActionItems[0].VM == "" {
				return "", fmt.Errorf("Error decoding content. First ActionItem is empty.")
			}
		}
	}
	// Now only support a single reservation each time.
	return se.ActionItems[0].VM, nil
}
