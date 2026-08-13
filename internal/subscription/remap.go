package subscription

// RemapSelection обновляет выбранный узел после refresh подписки.
// Если ID остался в next — без изменений; иначе remap по host:port:protocol;
// если endpoint исчез — selectedID сбрасывается (и счётчик удаляется).
// Если после этого выбор пуст и в next ровно один узел — выбирает его автоматически.
func RemapSelection(selectedID string, counts map[string]int, previous, next []Node) (string, map[string]int) {
	if selectedID != "" {
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
		selectedID = ""
	}
	return AutoSelectSingle(selectedID, counts, next)
}

// AutoSelectSingle выбирает единственный узел, если выбор пуст.
// NodeSelectCounts не меняет — автовыбор не считается ручным кликом.
func AutoSelectSingle(selectedID string, counts map[string]int, nodes []Node) (string, map[string]int) {
	if selectedID != "" || len(nodes) != 1 {
		return selectedID, counts
	}
	id := nodes[0].ID
	if id == "" {
		return "", counts
	}
	return id, counts
}
