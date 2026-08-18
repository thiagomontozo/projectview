import { describe, expect, it } from 'vitest';
import {
  ERR_CYCLE,
  ERR_DIV0,
  ERR_NAME,
  ERR_VALUE,
  columnName,
  evaluateFormula,
  evaluateSheet,
  formatValue
} from '../lib/formula';
import type { SheetCell } from '../types';

/**
 * The formula engine is hand-written, which means it is the part of the
 * spreadsheet most likely to be quietly wrong — a wrong total looks exactly
 * like a right one. These tests are therefore about arithmetic that must not
 * drift, and about the cases where the honest answer is an error rather than a
 * number.
 */

const sheet: Record<string, SheetCell> = {
  A1: { v: 'Hours' },
  A2: { v: '8' },
  A3: { v: '7.5' },
  A4: { v: '4' },
  A5: { f: '=SUM(A2:A4)' },
  B1: { v: 'Rate' },
  B2: { v: '120' },
  C2: { f: '=A2*B2' }
};

describe('arithmetic', () => {
  it('follows precedence rather than reading left to right', () => {
    expect(evaluateFormula('=2+3*4', {})).toBe(14);
    expect(evaluateFormula('=(2+3)*4', {})).toBe(20);
    expect(evaluateFormula('=2^3^2', {})).toBe(512); // right-associative, as in every sheet
    expect(evaluateFormula('=-3+10', {})).toBe(7);
  });

  it('divides by zero as an error, not as Infinity', () => {
    // Infinity would render as a number and quietly poison every total above it.
    expect(evaluateFormula('=1/0', {})).toBe(ERR_DIV0);
  });
});

describe('references', () => {
  it('reads other cells and ranges', () => {
    const values = evaluateSheet(sheet);
    expect(values.A5).toBe(19.5);
    expect(values.C2).toBe(960);
  });

  it('treats an empty cell as empty and a heading as zero', () => {
    // A column of hours with "Hours" at the top is the ordinary case. Refusing
    // to total it would be refusing the way people actually build sheets.
    expect(evaluateFormula('=SUM(A1:A4)', sheet)).toBe(19.5);
    expect(evaluateFormula('=Z99+1', sheet)).toBe(1);
  });

  it('refuses a bare range where one value is needed', () => {
    expect(evaluateFormula('=A2:A4', sheet)).toBe(ERR_VALUE);
  });
});

describe('functions', () => {
  it('computes the ones a project sheet actually uses', () => {
    expect(evaluateFormula('=AVERAGE(A2:A4)', sheet)).toBe(6.5);
    expect(evaluateFormula('=MIN(A2:A4)', sheet)).toBe(4);
    expect(evaluateFormula('=MAX(A2:A4)', sheet)).toBe(8);
    expect(evaluateFormula('=COUNT(A1:A4)', sheet)).toBe(3);
    expect(evaluateFormula('=COUNTA(A1:A4)', sheet)).toBe(4);
    expect(evaluateFormula('=ROUND(7.456,1)', {})).toBe(7.5);
    expect(evaluateFormula('=IF(A2>5,"over","under")', sheet)).toBe('over');
    expect(evaluateFormula('=CONCAT(A1," total")', sheet)).toBe('Hours total');
  });

  it('accepts a semicolon as an argument separator', () => {
    // How a Portuguese or German locale types it. Refusing would be refusing
    // half the people who will use this.
    expect(evaluateFormula('=ROUND(7.456;1)', {})).toBe(7.5);
  });

  it('names an unsupported function instead of pretending it is zero', () => {
    // The important half of being a small engine: what it cannot do, it says.
    expect(evaluateFormula('=VLOOKUP(A2,B1:B9,2)', sheet)).toBe(ERR_NAME);
    expect(evaluateFormula('=SUM(A2:A4', sheet)).toBe(ERR_VALUE);
  });
});

describe('cycles', () => {
  it('answers #CYCLE! instead of overflowing the stack', () => {
    const looping: Record<string, SheetCell> = {
      A1: { f: '=B1+1' },
      B1: { f: '=A1+1' },
      C1: { f: '=C1' }
    };
    const values = evaluateSheet(looping);
    expect(values.A1).toBe(ERR_CYCLE);
    expect(values.C1).toBe(ERR_CYCLE);
  });
});

describe('display', () => {
  it('names columns past Z', () => {
    expect(columnName(0)).toBe('A');
    expect(columnName(25)).toBe('Z');
    expect(columnName(26)).toBe('AA');
    expect(columnName(51)).toBe('AZ');
  });

  it('does not show floating-point noise', () => {
    // 0.1 + 0.2 is 0.30000000000000004, and a sheet showing that is a sheet
    // people stop trusting.
    expect(formatValue(evaluateFormula('=0.1+0.2', {}))).toBe('0.3');
    expect(formatValue('')).toBe('');
  });
});
