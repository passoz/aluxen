package application

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/passoz/aluxen/internal/domain"
	"github.com/passoz/aluxen/internal/domain/format"
)

// Calendário de ocupação: visão de leitura sobre os contratos que já existem.
// Nada é gravado — o administrador de temporada pensa em período por imóvel, e é
// isso que a tela mostra.

// CalendarPeriod é um período ocupado de um imóvel.
type CalendarPeriod struct {
	LeaseID      string
	TenantName   string
	PropertyID   string
	PropertyName string
	StartDate    string
	EndDate      string
	Status       domain.LeaseStatus
	StatusLabel  string
	RentAmount   float64
	// StartsHere e EndsHere indicam se o período começa ou termina no mês exibido,
	// para a barra não parecer que começa do nada.
	StartsHere bool
	EndsHere   bool
}

// CalendarDay é um dia do mês na grade de ocupação do imóvel.
type CalendarDay struct {
	Day        int
	Status     domain.LeaseStatus
	TenantName string
	// Starts e Ends marcam o primeiro e o último dia do período dentro do mês.
	Starts bool
	Ends   bool
}

// Occupied informa se o dia está tomado por contrato que bloqueia sobreposição.
func (d CalendarDay) Occupied() bool { return d.Status.BlocksOverlap() }

// CalendarProperty agrupa os períodos de um imóvel.
type CalendarProperty struct {
	PropertyID   string
	PropertyName string
	City         string
	State        string
	Periods      []CalendarPeriod
	// Days é a grade de ocupação do mês, um item por dia.
	Days []CalendarDay
	// OccupiedDays é quantos dias do mês exibido estão ocupados por contrato
	// ativo ou de temporada.
	OccupiedDays int
}

// CalendarMonth é o mês exibido no calendário.
type CalendarMonth struct {
	Year      int
	Month     int
	Label     string
	Days      int
	FirstWeek int
	Previous  string
	Next      string
}

// CalendarView é o corpo do calendário de ocupação.
type CalendarView struct {
	Month      CalendarMonth
	Properties []CalendarProperty
}

var calendarMonthLabels = []string{
	"janeiro", "fevereiro", "março", "abril", "maio", "junho",
	"julho", "agosto", "setembro", "outubro", "novembro", "dezembro",
}

// BuildCalendar monta o calendário de ocupação do mês informado.
func BuildCalendar(ctx context.Context, deps Deps, year, month int) (*CalendarView, error) {
	if month < 1 || month > 12 {
		now := deps.Clock.Now()
		year, month = now.Year(), int(now.Month())
	}
	if year < 1970 || year > 9999 {
		year = deps.Clock.Now().Year()
	}

	properties, err := ListProperties(ctx, deps)
	if err != nil {
		return nil, err
	}
	leases, err := ListLeases(ctx, deps)
	if err != nil {
		return nil, err
	}

	monthStart := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.Local)
	monthEnd := monthStart.AddDate(0, 1, -1)
	startISO := format.FormatLocalDate(monthStart)
	endISO := format.FormatLocalDate(monthEnd)

	byProperty := map[string][]CalendarPeriod{}
	occupied := map[string]int{}
	for _, lease := range leases {
		if lease.Status == domain.LeaseStatusTerminated {
			continue
		}
		if !domain.Overlaps(lease.StartDate, lease.EndDate, startISO, endISO) {
			continue
		}
		byProperty[lease.PropertyID] = append(byProperty[lease.PropertyID], CalendarPeriod{
			LeaseID:      lease.ID,
			TenantName:   lease.TenantName,
			PropertyID:   lease.PropertyID,
			PropertyName: lease.PropertyName,
			StartDate:    lease.StartDate,
			EndDate:      lease.EndDate,
			Status:       lease.Status,
			StatusLabel:  domain.LeaseStatusLabel(lease.Status),
			RentAmount:   lease.RentAmount,
			StartsHere:   lease.StartDate >= startISO,
			EndsHere:     lease.EndDate <= endISO,
		})
		if lease.Status.BlocksOverlap() {
			occupied[lease.PropertyID] += overlapDays(lease.StartDate, lease.EndDate, monthStart, monthEnd)
		}
	}

	previousYear, previousMonthNumber := previousMonth(year, month)
	nextYear, nextMonthNumber := nextMonth(year, month)
	view := &CalendarView{
		Month: CalendarMonth{
			Year:      year,
			Month:     month,
			Label:     fmt.Sprintf("%s de %d", calendarMonthLabels[month-1], year),
			Days:      domain.DaysInMonth(year, month),
			FirstWeek: int(monthStart.Weekday()),
			Previous:  fmt.Sprintf("/dashboard?tab=calendar&year=%d&month=%d", previousYear, previousMonthNumber),
			Next:      fmt.Sprintf("/dashboard?tab=calendar&year=%d&month=%d", nextYear, nextMonthNumber),
		},
	}

	daysInMonth := domain.DaysInMonth(year, month)
	for _, property := range properties {
		periods := byProperty[property.ID]
		sort.Slice(periods, func(a, b int) bool { return periods[a].StartDate < periods[b].StartDate })
		view.Properties = append(view.Properties, CalendarProperty{
			PropertyID:   property.ID,
			PropertyName: property.Name,
			City:         property.City,
			State:        property.State,
			Periods:      periods,
			Days:         buildCalendarDays(periods, year, month, daysInMonth),
			OccupiedDays: occupied[property.ID],
		})
	}
	return view, nil
}

// buildCalendarDays monta a grade de dias do mês EXIBIDO a partir dos períodos
// do imóvel. Um dia sem período fica livre; quando dois períodos se tocam, o que
// bloqueia sobreposição prevalece (é o que ocupa de fato).
//
// O dia é construído com o ano/mês exibidos, e não com o mês do início do
// período: um contrato que começa em agosto e termina em setembro precisa pintar
// os dias de setembro, não repetir agosto.
func buildCalendarDays(periods []CalendarPeriod, year, month, daysInMonth int) []CalendarDay {
	days := make([]CalendarDay, 0, daysInMonth)
	for day := 1; day <= daysInMonth; day++ {
		entry := CalendarDay{Day: day}
		date := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.Local)
		for _, period := range periods {
			start := format.ParseLocalDate(period.StartDate)
			end := format.ParseLocalDate(period.EndDate)
			if date.Before(start) || date.After(end) {
				continue
			}
			if entry.Status.BlocksOverlap() && !period.Status.BlocksOverlap() {
				continue
			}
			entry.Status = period.Status
			entry.TenantName = period.TenantName
			entry.Starts = !date.After(start)
			entry.Ends = !date.Before(end)
		}
		days = append(days, entry)
	}
	return days
}

// overlapDays conta quantos dias do mês o período ocupa.
func overlapDays(startDate, endDate string, monthStart, monthEnd time.Time) int {
	start := format.ParseLocalDate(startDate)
	end := format.ParseLocalDate(endDate)
	if start.Before(monthStart) {
		start = monthStart
	}
	if end.After(monthEnd) {
		end = monthEnd
	}
	if end.Before(start) {
		return 0
	}
	return int(end.Sub(start).Hours()/24) + 1
}

func previousMonth(year, month int) (int, int) {
	if month == 1 {
		return year - 1, 12
	}
	return year, month - 1
}

func nextMonth(year, month int) (int, int) {
	if month == 12 {
		return year + 1, 1
	}
	return year, month + 1
}
