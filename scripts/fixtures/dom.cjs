// Minimal DOM contracts for offline view tests; this does not model layout.
class Element {
  constructor(tag, attrs = {}, ...children) {
    this.tagName = tag.toUpperCase();
    this.attrs = attrs;
    Object.assign(this, attrs);
    this.children = [];
    this.listeners = {};
    this.dataset = {};
    this.value = attrs.value ?? '';
    this.disabled = attrs.disabled ?? false;
    this.open = false;
    this.isConnected = true;
    this.scrollTop = 0;
    for (const [key, value] of Object.entries(attrs)) {
      if (key.startsWith('data-')) this.dataset[key.slice(5).replace(/-([a-z])/g, (_, c) => c.toUpperCase())] = String(value);
    }
    this.classes = new Set((attrs.class || '').split(' ').filter(Boolean));
    this.classList = {
      add: (...names) => names.forEach(name => this.classes.add(name)),
      remove: (...names) => names.forEach(name => this.classes.delete(name)),
      contains: name => this.classes.has(name),
      toggle: (name, force = !this.classes.has(name)) => {
        if (force) this.classes.add(name); else this.classes.delete(name);
        return force;
      },
    };
    this.append(...children);
  }
  append(...children) {
    for (const child of children.flat().filter(value => value != null)) {
      if (child instanceof Element) child.parent = child.parentElement = this;
      this.children.push(child);
    }
  }
  prepend(...children) { const old = this.children; this.children = []; this.append(...children, ...old); }
  replaceChildren(...children) { this.children = []; this.append(...children); }
  set textContent(value) { this.children = [value]; }
  get textContent() { return this.children.map(value => value instanceof Element ? value.textContent : String(value ?? '')).join(''); }
  get lastElementChild() { return this.children.findLast(value => value instanceof Element); }
  addEventListener(type, listener) { (this.listeners[type] ??= []).push(listener); }
  emit(type, event = {preventDefault() {}}) {
    let result;
    for (const listener of this.listeners[type] || []) result = listener(event);
    return result;
  }
  querySelectorAll(selector) {
    return this.children.filter(value => value instanceof Element).flatMap(child => [
      ...(matches(child, selector) ? [child] : []), ...child.querySelectorAll(selector),
    ]);
  }
  querySelector(selector) {
    if (selector === ':scope > summary') return this.children.find(value => value instanceof Element && value.tagName === 'SUMMARY') || null;
    return this.querySelectorAll(selector)[0] || null;
  }
  checkValidity() { return true; }
  showModal() { this.open = true; }
  close() { this.open = false; this.emit('close'); }
  remove() {
    this.isConnected = false;
    if (this.parentElement) this.parentElement.children = this.parentElement.children.filter(value => value !== this);
  }
}
function matches(node, selector) {
  return selector.startsWith('.') ? node.classes.has(selector.slice(1)) :
    selector.startsWith('#') ? node.attrs.id === selector.slice(1) : node.tagName === selector.toUpperCase();
}
const el = (...args) => new Element(...args);
const button = (label, click) => { const node = el('button', {}, label); node.click = click; return node; };
const all = node => [node, ...node.children.filter(value => value instanceof Element).flatMap(all)];
const find = (node, predicate) => all(node).find(predicate);
const byText = (node, text) => find(node, value => value.tagName === 'BUTTON' && value.textContent === text);
const field = (node, label) => find(node, value => value.attrs['aria-label'] === label);
const get = (node, id) => find(node, value => value.id === id || value.attrs.id === id);
const clone = value => JSON.parse(JSON.stringify(value));
module.exports = {Element, el, button, all, find, byText, field, get, clone};
