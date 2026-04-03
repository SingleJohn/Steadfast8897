import { defineComponent, h } from 'vue'

export const GDriveIcon = defineComponent({
  name: 'GDriveIcon',
  setup() {
    return () =>
      h(
        'svg',
        {
          xmlns: 'http://www.w3.org/2000/svg',
          viewBox: '0 0 1024 1024',
          width: '1em',
          height: '1em',
        },
        [
          h('path', {
            d: 'M170.666667 960.170667l170.666666-298.666667h682.666667l-170.666667 298.666667H170.666667z',
            fill: '#2E67F5',
          }),
          h('path', {
            d: 'M682.666667 661.504h341.333333L682.666667 64.170667H341.333333l341.333334 597.333333z',
            fill: '#F7C034',
          }),
          h('path', {
            d: 'M0 661.504l170.666667 298.666667 341.333333-597.333334-170.666667-298.666666-341.333333 597.333333z',
            fill: '#18984D',
          }),
        ],
      )
  },
})
