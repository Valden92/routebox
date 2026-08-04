package subscription

// RemapSelection обновляет выбранный узел после refresh подписки.
// Если ID остался в next — без изменений; иначе remap по host:port:protocol;
// если endpoint исчез — selectedID сбрасывается (и счётчик удаляется).
func RemapSelection(selectedID string, counts map[string]int, previous, next []Node) (string, map[string]int) {
	if selectedID == "" {
		return "", counts
	}
	if _, ok := FindNodeByID(next, selectedID); ok {
		return selectedID, counts
	}
	if old, ok := FindNodeByID(previous, selectedID); ok {
		if remapped, ok := FindNodeByEndpoint(next, old.Host, old.Port, old.Protocol); ok {
			if counts == nil {
				counts = make(map[string]int)
			}
			counts[remapped.ID] += counts[selectedID]
			delete(counts, selectedID)
			return remapped.ID, counts
		}
	}
	if counts != nil {
		delete(counts, selectedID)
	}
	return "", counts
}
