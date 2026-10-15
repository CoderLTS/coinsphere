package service

import "sort"

func uniquePositiveInt64s(values []int64) []int64 {
	set := map[int64]bool{}
	for _, value := range values {
		if value > 0 {
			set[value] = true
		}
	}
	result := make([]int64, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
