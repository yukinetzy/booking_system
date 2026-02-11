package utils

import "strconv"

type Pagination struct {
	Page  int
	Limit int
	Skip  int64
}

type PaginationMeta struct {
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"totalPages"`
	HasPrev    bool  `json:"hasPrev"`
	HasNext    bool  `json:"hasNext"`
	PrevPage   *int  `json:"prevPage"`
	NextPage   *int  `json:"nextPage"`
}

func parsePositiveInt(value string, fallback int) int {
	number, err := strconv.Atoi(value)
	if err != nil || number <= 0 {
		return fallback
	}
	return number
}

func GetPagination(pageValue, limitValue string, defaultLimit, maxLimit int) Pagination {
	page := parsePositiveInt(pageValue, 1)
	limit := parsePositiveInt(limitValue, defaultLimit)
	if limit > maxLimit {
		limit = maxLimit
	}

	return Pagination{
		Page:  page,
		Limit: limit,
		Skip:  int64((page - 1) * limit),
	}
}

func GetPaginationMeta(total int64, page, limit int) PaginationMeta {
	totalPages := int((total + int64(limit) - 1) / int64(limit))
	if totalPages < 1 {
		totalPages = 1
	}

	var prevPage *int
	var nextPage *int
	if page > 1 {
		value := page - 1
		prevPage = &value
	}
	if page < totalPages {
		value := page + 1
		nextPage = &value
	}

	return PaginationMeta{
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: totalPages,
		HasPrev:    page > 1,
		HasNext:    page < totalPages,
		PrevPage:   prevPage,
		NextPage:   nextPage,
	}
}
