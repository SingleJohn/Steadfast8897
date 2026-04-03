import { defineComponent, h } from 'vue'

export const PathMappingIcon = defineComponent({
  name: 'PathMappingIcon',
  setup() {
    return () =>
      h(
        'svg',
        {
          xmlns: 'http://www.w3.org/2000/svg',
          viewBox: '0 0 24 24',
          fill: 'none',
          stroke: 'currentColor',
          'stroke-width': '2',
          'stroke-linecap': 'round',
          'stroke-linejoin': 'round',
        },
        [
          h('path', { d: 'M4 8h13' }),
          h('path', { d: 'M14 5l3 3-3 3' }),
          h('path', { d: 'M20 16H7' }),
          h('path', { d: 'M10 13l-3 3 3 3' }),
        ],
      )
  },
})
