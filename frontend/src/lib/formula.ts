import type { SheetCell } from '../types';

/**
 * A spreadsheet formula evaluator.
 *
 * Written here rather than pulled in, and that is a deliberate trade. The
 * grid-and-formula libraries that would do this properly (Univer, HyperFormula,
 * Handsontable) are each larger than this entire application's bundle, and two
 * of the three are commercially licensed for exactly this use. What people
 * actually reach for in a project tool is a column of numbers with a total at
 * the bottom, so that is what this does — completely and predictably — and it
 * says plainly what it does not do.
 *
 * Supported: numbers, text, cell references (A1), ranges (A1:B9), the four
 * arithmetic operators with correct precedence, parentheses, unary minus,
 * comparison operators, and the functions below. Not supported: absolute
 * references, cross-sheet references, arrays, dates as first-class values,
 * anything iterative. A formula using one of those gets #NAME? rather than a
 * wrong number, which is the important half.
 */

/** What a cell evaluates to. Strings starting with # are errors, as in every sheet. */
export type CellValue = number | string | boolean;

export const ERR_CYCLE = '#CYCLE!';
export const ERR_NAME = '#NAME?';
export const ERR_VALUE = '#VALUE!';
export const ERR_DIV0 = '#DIV/0!';
export const ERR_REF = '#REF!';

/** Converts a zero-based column index to its letters: 0 → A, 26 → AA. */
export function columnName(index: number): string {
  let name = '';
  let n = index;
  while (n >= 0) {
    name = String.fromCharCode((n % 26) + 65) + name;
    n = Math.floor(n / 26) - 1;
  }
  return name;
}

export function columnIndex(name: string): number {
  let index = 0;
  for (const char of name.toUpperCase()) {
    index = index * 26 + (char.charCodeAt(0) - 64);
  }
  return index - 1;
}

export function cellRef(row: number, col: number): string {
  return `${columnName(col)}${row + 1}`;
}

/* --- Tokeniser -------------------------------------------------------------- */

type Token =
  | { type: 'number'; value: number }
  | { type: 'string'; value: string }
  | { type: 'ref'; value: string }
  | { type: 'range'; from: string; to: string }
  | { type: 'name'; value: string }
  | { type: 'op'; value: string }
  | { type: 'paren'; value: '(' | ')' }
  | { type: 'comma' };

const REF = /^[A-Za-z]{1,3}[0-9]{1,5}/;

function tokenise(input: string): Token[] {
  const tokens: Token[] = [];
  let i = 0;

  while (i < input.length) {
    const char = input[i];

    if (char === ' ') {
      i += 1;
      continue;
    }
    if (char === '(' || char === ')') {
      tokens.push({ type: 'paren', value: char });
      i += 1;
      continue;
    }
    if (char === ',' || char === ';') {
      // Semicolons too: a Portuguese or German locale writes SUM(A1;A2), and
      // refusing that would be refusing how half the users type.
      tokens.push({ type: 'comma' });
      i += 1;
      continue;
    }
    if (char === '"') {
      const end = input.indexOf('"', i + 1);
      if (end < 0) throw new Error(ERR_VALUE);
      tokens.push({ type: 'string', value: input.slice(i + 1, end) });
      i = end + 1;
      continue;
    }
    if (/[0-9.]/.test(char)) {
      const match = /^[0-9]*\.?[0-9]+([eE][-+]?[0-9]+)?/.exec(input.slice(i));
      if (!match) throw new Error(ERR_VALUE);
      tokens.push({ type: 'number', value: Number(match[0]) });
      i += match[0].length;
      continue;
    }
    if (/[A-Za-z_]/.test(char)) {
      const rest = input.slice(i);
      const ref = REF.exec(rest);
      // A reference only if what follows cannot make it a name: SUM( is a
      // function, A1 is a cell, and LOG10 is neither a cell nor supported.
      if (ref && !/^[A-Za-z0-9_]/.test(rest.slice(ref[0].length))) {
        const after = rest.slice(ref[0].length);
        const rangeEnd = /^:([A-Za-z]{1,3}[0-9]{1,5})/.exec(after);
        if (rangeEnd) {
          tokens.push({ type: 'range', from: ref[0].toUpperCase(), to: rangeEnd[1].toUpperCase() });
          i += ref[0].length + rangeEnd[0].length;
          continue;
        }
        tokens.push({ type: 'ref', value: ref[0].toUpperCase() });
        i += ref[0].length;
        continue;
      }
      const name = /^[A-Za-z_][A-Za-z0-9_.]*/.exec(rest);
      if (!name) throw new Error(ERR_NAME);
      tokens.push({ type: 'name', value: name[0].toUpperCase() });
      i += name[0].length;
      continue;
    }

    const twoChar = input.slice(i, i + 2);
    if (twoChar === '<=' || twoChar === '>=' || twoChar === '<>') {
      tokens.push({ type: 'op', value: twoChar });
      i += 2;
      continue;
    }
    if ('+-*/^&<>='.includes(char)) {
      tokens.push({ type: 'op', value: char });
      i += 1;
      continue;
    }
    throw new Error(ERR_NAME);
  }
  return tokens;
}

/* --- Evaluation -------------------------------------------------------------- */

interface Context {
  cells: Record<string, SheetCell>;
  /** References currently being evaluated, which is how a cycle is caught. */
  visiting: Set<string>;
  cache: Map<string, CellValue>;
}

function toNumber(value: CellValue): number {
  if (typeof value === 'number') return value;
  if (typeof value === 'boolean') return value ? 1 : 0;
  const trimmed = value.trim();
  if (trimmed === '') return 0;
  // Text that is not a number contributes zero rather than poisoning the sum:
  // a column of hours with a heading is the ordinary case, not an error.
  const parsed = Number(trimmed.replace(',', '.'));
  return Number.isFinite(parsed) ? parsed : 0;
}

function isError(value: CellValue): value is string {
  return typeof value === 'string' && value.startsWith('#');
}

function expand(from: string, to: string): string[] {
  const start = /^([A-Za-z]{1,3})([0-9]{1,5})$/.exec(from);
  const end = /^([A-Za-z]{1,3})([0-9]{1,5})$/.exec(to);
  if (!start || !end) return [];

  const [c1, c2] = [columnIndex(start[1]), columnIndex(end[1])].sort((a, b) => a - b);
  const [r1, r2] = [Number(start[2]), Number(end[2])].sort((a, b) => a - b);

  const refs: string[] = [];
  for (let row = r1; row <= r2; row += 1) {
    for (let col = c1; col <= c2; col += 1) {
      refs.push(`${columnName(col)}${row}`);
    }
  }
  return refs;
}

const FUNCTIONS: Record<string, (args: CellValue[][]) => CellValue> = {
  SUM: (args) => flat(args).reduce<number>((total, value) => total + toNumber(value), 0),
  AVERAGE: (args) => {
    const numbers = flat(args).filter((v) => v !== '');
    if (numbers.length === 0) return ERR_DIV0;
    return numbers.reduce<number>((total, value) => total + toNumber(value), 0) / numbers.length;
  },
  COUNT: (args) => flat(args).filter((v) => typeof v === 'number' || (v !== '' && Number.isFinite(Number(v)))).length,
  COUNTA: (args) => flat(args).filter((v) => v !== '').length,
  MIN: (args) => {
    const numbers = flat(args).map(toNumber);
    return numbers.length ? Math.min(...numbers) : 0;
  },
  MAX: (args) => {
    const numbers = flat(args).map(toNumber);
    return numbers.length ? Math.max(...numbers) : 0;
  },
  ROUND: (args) => {
    const [value, places = 0] = flat(args).map(toNumber);
    const factor = 10 ** places;
    return Math.round(value * factor) / factor;
  },
  ABS: (args) => Math.abs(toNumber(flat(args)[0] ?? 0)),
  IF: (args) => {
    const condition = args[0]?.[0];
    const truthy = typeof condition === 'boolean' ? condition : toNumber(condition ?? 0) !== 0;
    const branch = truthy ? args[1] : args[2];
    return branch?.[0] ?? '';
  },
  CONCAT: (args) => flat(args).map((v) => String(v)).join(''),
  LEN: (args) => String(flat(args)[0] ?? '').length,
  TODAY: () => new Date().toISOString().slice(0, 10)
};

function flat(args: CellValue[][]): CellValue[] {
  return args.flat();
}

/**
 * A recursive-descent parser. Precedence, lowest first: comparison, then & and
 * +/-, then * and /, then ^, then unary minus.
 */
function parse(tokens: Token[], ctx: Context): CellValue {
  let position = 0;

  function peek(): Token | undefined {
    return tokens[position];
  }

  function comparison(): CellValue {
    let left = concat();
    for (;;) {
      const token = peek();
      if (token?.type !== 'op' || !['=', '<', '>', '<=', '>=', '<>'].includes(token.value)) return left;
      position += 1;
      const right = concat();
      if (isError(left)) return left;
      if (isError(right)) return right;

      const l = typeof left === 'string' && typeof right === 'string' ? left : toNumber(left);
      const rgt = typeof left === 'string' && typeof right === 'string' ? right : toNumber(right);
      switch (token.value) {
        case '=':
          left = l === rgt;
          break;
        case '<>':
          left = l !== rgt;
          break;
        case '<':
          left = l < rgt;
          break;
        case '>':
          left = l > rgt;
          break;
        case '<=':
          left = l <= rgt;
          break;
        default:
          left = l >= rgt;
      }
    }
  }

  function concat(): CellValue {
    let left = additive();
    for (;;) {
      const token = peek();
      if (token?.type !== 'op' || token.value !== '&') return left;
      position += 1;
      const right = additive();
      if (isError(left)) return left;
      if (isError(right)) return right;
      left = String(left) + String(right);
    }
  }

  function additive(): CellValue {
    let left = multiplicative();
    for (;;) {
      const token = peek();
      if (token?.type !== 'op' || (token.value !== '+' && token.value !== '-')) return left;
      position += 1;
      const right = multiplicative();
      if (isError(left)) return left;
      if (isError(right)) return right;
      left = token.value === '+' ? toNumber(left) + toNumber(right) : toNumber(left) - toNumber(right);
    }
  }

  function multiplicative(): CellValue {
    let left = power();
    for (;;) {
      const token = peek();
      if (token?.type !== 'op' || (token.value !== '*' && token.value !== '/')) return left;
      position += 1;
      const right = power();
      if (isError(left)) return left;
      if (isError(right)) return right;
      if (token.value === '/' && toNumber(right) === 0) return ERR_DIV0;
      left = token.value === '*' ? toNumber(left) * toNumber(right) : toNumber(left) / toNumber(right);
    }
  }

  function power(): CellValue {
    const left = unary();
    const token = peek();
    if (token?.type !== 'op' || token.value !== '^') return left;
    position += 1;
    const right = power();
    if (isError(left)) return left;
    if (isError(right)) return right;
    return toNumber(left) ** toNumber(right);
  }

  function unary(): CellValue {
    const token = peek();
    if (token?.type === 'op' && (token.value === '-' || token.value === '+')) {
      position += 1;
      const value = unary();
      if (isError(value)) return value;
      return token.value === '-' ? -toNumber(value) : toNumber(value);
    }
    return primary();
  }

  /** Values are returned as lists because a range is one argument of many cells. */
  function argument(): CellValue[] {
    const token = peek();
    if (token?.type === 'range') {
      position += 1;
      return expand(token.from, token.to).map((ref) => resolve(ref, ctx));
    }
    return [comparison()];
  }

  function primary(): CellValue {
    const token = peek();
    if (!token) throw new Error(ERR_VALUE);

    if (token.type === 'number') {
      position += 1;
      return token.value;
    }
    if (token.type === 'string') {
      position += 1;
      return token.value;
    }
    if (token.type === 'ref') {
      position += 1;
      return resolve(token.value, ctx);
    }
    if (token.type === 'range') {
      // A bare range outside a function has no single value. Excel answers
      // #VALUE! here and so does this.
      position += 1;
      return ERR_VALUE;
    }
    if (token.type === 'paren' && token.value === '(') {
      position += 1;
      const value = comparison();
      const closing = peek();
      if (closing?.type !== 'paren' || closing.value !== ')') throw new Error(ERR_VALUE);
      position += 1;
      return value;
    }
    if (token.type === 'name') {
      position += 1;
      const fn = FUNCTIONS[token.value];
      const open = peek();
      if (open?.type !== 'paren' || open.value !== '(') {
        // TRUE and FALSE are the only bare names with a meaning.
        if (token.value === 'TRUE') return true;
        if (token.value === 'FALSE') return false;
        return ERR_NAME;
      }
      position += 1;

      const args: CellValue[][] = [];
      if (peek()?.type === 'paren' && (peek() as { value: string }).value === ')') {
        position += 1;
      } else {
        for (;;) {
          args.push(argument());
          const next = peek();
          if (next?.type === 'comma') {
            position += 1;
            continue;
          }
          if (next?.type === 'paren' && next.value === ')') {
            position += 1;
            break;
          }
          throw new Error(ERR_VALUE);
        }
      }

      // An unknown name is named as unknown rather than treated as empty: a
      // sheet that silently reads VLOOKUP as zero is worse than one that says
      // it cannot do VLOOKUP.
      if (!fn) return ERR_NAME;
      const failed = args.flat().find(isError);
      if (failed) return failed;
      return fn(args);
    }
    throw new Error(ERR_VALUE);
  }

  const result = comparison();
  // Trailing rubbish means the formula was not understood, whatever the prefix
  // happened to evaluate to.
  if (position !== tokens.length) return ERR_VALUE;
  return result;
}

/**
 * Evaluates one cell, following references.
 *
 * Cycles are caught by the visiting set rather than by a depth limit: a limit
 * turns =A1+1 in A1 into a slow stack overflow, and this turns it into #CYCLE!
 * immediately, which is what a person needs to see.
 */
function resolve(ref: string, ctx: Context): CellValue {
  if (ctx.cache.has(ref)) return ctx.cache.get(ref) as CellValue;
  if (ctx.visiting.has(ref)) return ERR_CYCLE;

  const cell = ctx.cells[ref];
  if (!cell) return '';

  let value: CellValue;
  if (cell.f) {
    ctx.visiting.add(ref);
    try {
      value = parse(tokenise(cell.f.replace(/^=/, '')), ctx);
    } catch (error) {
      value = error instanceof Error && error.message.startsWith('#') ? error.message : ERR_VALUE;
    } finally {
      ctx.visiting.delete(ref);
    }
  } else {
    const raw = cell.v ?? '';
    const asNumber = raw.trim() === '' ? NaN : Number(raw);
    value = Number.isFinite(asNumber) ? asNumber : raw;
  }

  ctx.cache.set(ref, value);
  return value;
}

/**
 * Evaluates every cell in a sheet.
 *
 * The whole sheet at once, with one cache: a sheet is small (the server refuses
 * anything past twenty thousand filled cells) and evaluating per cell on render
 * would re-walk the same dependency chain for every row on screen.
 */
export function evaluateSheet(cells: Record<string, SheetCell>): Record<string, CellValue> {
  const ctx: Context = { cells, visiting: new Set(), cache: new Map() };
  const out: Record<string, CellValue> = {};
  for (const ref of Object.keys(cells)) {
    out[ref] = resolve(ref, ctx);
  }
  return out;
}

/** Evaluates a single formula against a sheet. Used while somebody is typing. */
export function evaluateFormula(formula: string, cells: Record<string, SheetCell>): CellValue {
  const ctx: Context = { cells, visiting: new Set(), cache: new Map() };
  try {
    return parse(tokenise(formula.replace(/^=/, '')), ctx);
  } catch (error) {
    return error instanceof Error && error.message.startsWith('#') ? error.message : ERR_VALUE;
  }
}

/** Formats a computed value for display. Errors and text pass through as they are. */
export function formatValue(value: CellValue | undefined): string {
  if (value === undefined || value === '') return '';
  if (typeof value === 'boolean') return value ? 'TRUE' : 'FALSE';
  if (typeof value === 'number') {
    if (!Number.isFinite(value)) return ERR_VALUE;
    // Rounded for display only. The stored number keeps its precision, so a
    // total of displayed values still adds up to the displayed total.
    return String(Math.round(value * 1e10) / 1e10);
  }
  return value;
}
