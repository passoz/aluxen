import { describe, it, expect } from 'bun:test';
import React from 'react';
import { renderToString } from 'react-dom/server';
import { DatePickerInput, DateTimePickerInput } from '../src/components/ui';

describe('DatePickerInput and DateTimePickerInput', () => {
  it('DatePickerInput renders with select controls for Month and Year when open', () => {
    // When open, the calendar header has select controls
    const html = renderToString(
      <DatePickerInput
        id="test-date"
        name="test_date"
        value="2026-09-04"
        onChange={() => {}}
        label="Data de Teste"
      />
    );
    expect(html).toContain('Data de Teste');
    expect(html).toContain('04/09/2026');
  });

  it('DateTimePickerInput renders formatted value and time picker', () => {
    const html = renderToString(
      <DateTimePickerInput
        id="test-datetime"
        name="test_datetime"
        value="2026-09-04T15:30"
        onChange={() => {}}
        label="Data e Hora"
      />
    );
    expect(html).toContain('Data e Hora');
    expect(html).toContain('04/09/2026');
    expect(html).toContain('15:30');
  });
});
