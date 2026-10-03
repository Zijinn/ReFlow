import "@testing-library/jest-dom/vitest"

// jsdom performs no layout, so every element measures 0x0. @tanstack/react-virtual
// sizes its scroll viewport from `offsetWidth`/`offsetHeight` (virtual-core's
// `getRect`) and abandons range calculation entirely once that height is 0,
// mounting no rows at all. Give elements a non-zero box so virtualized lists
// render in tests. virtual-core itself needs no observer stub: it reads the rect
// synchronously and already guards the missing-observer case (the stub at the
// bottom of this file is for the note cell's width refit).
const VIEWPORT_HEIGHT = 900
const VIEWPORT_WIDTH = 1280

for (const [property, value] of [
  ["clientHeight", VIEWPORT_HEIGHT],
  ["clientWidth", VIEWPORT_WIDTH],
  ["offsetHeight", VIEWPORT_HEIGHT],
  ["offsetWidth", VIEWPORT_WIDTH],
] as const) {
  Object.defineProperty(HTMLElement.prototype, property, {
    configurable: true,
    get() {
      return value
    },
  })
}

HTMLElement.prototype.getBoundingClientRect = function getBoundingClientRect() {
  return {
    x: 0,
    y: 0,
    top: 0,
    left: 0,
    right: VIEWPORT_WIDTH,
    bottom: VIEWPORT_HEIGHT,
    width: VIEWPORT_WIDTH,
    height: VIEWPORT_HEIGHT,
    toJSON: () => ({}),
  }
}

// jsdom has no scrolling implementation; the workbench calls scrollIntoView to
// bring a calendar-focused submission row into view.
HTMLElement.prototype.scrollIntoView = function scrollIntoView() {}

// jsdom 没有指针捕获：拖拽把手（侧栏/时间线的 PaneDivider、表头的列宽把手）都靠
// 它把 pointermove 锁在自己身上。给一组空实现，测试才能一路走到 pointerup。
Element.prototype.setPointerCapture = function setPointerCapture() {}
Element.prototype.releasePointerCapture = function releasePointerCapture() {}
Element.prototype.hasPointerCapture = function hasPointerCapture() {
  return false
}

// jsdom 没有 ResizeObserver，而备注格的自动长高用它响应列宽拖拽，组件一挂载就会抛。
// 给一个永不触发的空实现即可：jsdom 不算布局，触发与否都改变不了测试断言的提交语义。
// 回调不收进构造函数——多传的参数在 JS 里本来就被忽略，声明了反而是未使用的形参。
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
  takeRecords(): ResizeObserverEntry[] {
    return []
  }
}
globalThis.ResizeObserver = ResizeObserverStub
