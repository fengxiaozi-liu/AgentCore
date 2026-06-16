package skill

import "fmt"

func errSkillNameRequired(index int) error {
	return fmt.Errorf("skill at index %d name is required", index)
}

func errSkillDescriptionRequired(name string) error {
	return fmt.Errorf("skill %q description is required", name)
}

func errSkillContentLoaderRequired(name string) error {
	return fmt.Errorf("skill %q content loader is required", name)
}

func errSkillDuplicated(name string) error {
	return fmt.Errorf("skill %q is duplicated", name)
}
