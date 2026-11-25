// Helpers for the JavaScript of core.script (ADR 0028), loaded with
// require('./scripts/util.js'). Only files in scripts/ of the project can be loaded.

// round2 rounds to two places, e.g. for amounts.
exports.round2 = (n) => Math.round(n * 100) / 100;

// sum adds a field of a list of objects; decimal values come as text, Number() reads them.
exports.sum = (rows, field) => rows.reduce((s, r) => s + Number(r[field]), 0);
