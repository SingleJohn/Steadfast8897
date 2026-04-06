import { defineComponent, h } from 'vue'

// Netflix - Red "N" ribbon
export const NetflixIcon = defineComponent({
  name: 'NetflixIcon',
  setup() {
    return () =>
      h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 48 48', width: '1em', height: '1em' }, [
        h('path', { d: 'M6 3v42l12-18V3H6z', fill: '#E50914' }),
        h('path', { d: 'M30 3v42l12-18V3H30z', fill: '#E50914' }),
        h('path', { d: 'M6 3l18 42h12L18 3H6z', fill: '#E50914' }),
      ])
  },
})

// Disney+ - Blue castle silhouette
export const DisneyPlusIcon = defineComponent({
  name: 'DisneyPlusIcon',
  setup() {
    return () =>
      h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 48 48', width: '1em', height: '1em' }, [
        h('rect', { x: '2', y: '2', width: '44', height: '44', rx: '8', fill: '#0D1B63' }),
        h('text', { x: '24', y: '32', fill: '#fff', 'font-size': '20', 'font-weight': 'bold', 'text-anchor': 'middle', 'font-family': 'Arial' }, 'D+'),
      ])
  },
})

// HBO - Black background with white "HBO"
export const HBOIcon = defineComponent({
  name: 'HBOIcon',
  setup() {
    return () =>
      h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 48 48', width: '1em', height: '1em' }, [
        h('rect', { x: '2', y: '2', width: '44', height: '44', rx: '8', fill: '#000' }),
        h('text', { x: '24', y: '30', fill: '#fff', 'font-size': '14', 'font-weight': 'bold', 'text-anchor': 'middle', 'font-family': 'Arial' }, 'HBO'),
      ])
  },
})

// Apple TV+ - Black with white Apple logo abstraction
export const AppleTVIcon = defineComponent({
  name: 'AppleTVIcon',
  setup() {
    return () =>
      h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 48 48', width: '1em', height: '1em' }, [
        h('rect', { x: '2', y: '2', width: '44', height: '44', rx: '8', fill: '#1d1d1f' }),
        h('text', { x: '24', y: '30', fill: '#fff', 'font-size': '12', 'font-weight': 'bold', 'text-anchor': 'middle', 'font-family': 'Arial' }, 'tv+'),
      ])
  },
})

// Amazon - Orange arrow on dark
export const AmazonIcon = defineComponent({
  name: 'AmazonIcon',
  setup() {
    return () =>
      h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 48 48', width: '1em', height: '1em' }, [
        h('rect', { x: '2', y: '2', width: '44', height: '44', rx: '8', fill: '#00A8E1' }),
        h('text', { x: '24', y: '30', fill: '#fff', 'font-size': '10', 'font-weight': 'bold', 'text-anchor': 'middle', 'font-family': 'Arial' }, 'Prime'),
      ])
  },
})

// Hulu - Green
export const HuluIcon = defineComponent({
  name: 'HuluIcon',
  setup() {
    return () =>
      h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 48 48', width: '1em', height: '1em' }, [
        h('rect', { x: '2', y: '2', width: '44', height: '44', rx: '8', fill: '#1CE783' }),
        h('text', { x: '24', y: '31', fill: '#040405', 'font-size': '13', 'font-weight': 'bold', 'text-anchor': 'middle', 'font-family': 'Arial' }, 'hulu'),
      ])
  },
})

// Paramount+ - Blue mountain
export const ParamountIcon = defineComponent({
  name: 'ParamountIcon',
  setup() {
    return () =>
      h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 48 48', width: '1em', height: '1em' }, [
        h('rect', { x: '2', y: '2', width: '44', height: '44', rx: '8', fill: '#0064FF' }),
        h('text', { x: '24', y: '30', fill: '#fff', 'font-size': '12', 'font-weight': 'bold', 'text-anchor': 'middle', 'font-family': 'Arial' }, 'P+'),
      ])
  },
})

// Peacock - Purple/gradient
export const PeacockIcon = defineComponent({
  name: 'PeacockIcon',
  setup() {
    return () =>
      h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 48 48', width: '1em', height: '1em' }, [
        h('rect', { x: '2', y: '2', width: '44', height: '44', rx: '8', fill: '#000' }),
        h('text', { x: '24', y: '30', fill: '#F4B400', 'font-size': '10', 'font-weight': 'bold', 'text-anchor': 'middle', 'font-family': 'Arial' }, 'Peacock'),
      ])
  },
})

// Crunchyroll - Orange
export const CrunchyrollIcon = defineComponent({
  name: 'CrunchyrollIcon',
  setup() {
    return () =>
      h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 48 48', width: '1em', height: '1em' }, [
        h('rect', { x: '2', y: '2', width: '44', height: '44', rx: '8', fill: '#F47521' }),
        h('text', { x: '24', y: '30', fill: '#fff', 'font-size': '11', 'font-weight': 'bold', 'text-anchor': 'middle', 'font-family': 'Arial' }, 'CR'),
      ])
  },
})

// Generic platform icon
export const GenericPlatformIcon = defineComponent({
  name: 'GenericPlatformIcon',
  setup() {
    return () =>
      h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 48 48', width: '1em', height: '1em' }, [
        h('rect', { x: '2', y: '2', width: '44', height: '44', rx: '8', fill: '#555' }),
        h('path', { d: 'M16 14h16v4H16zM16 22h16v4H16zM16 30h10v4H16z', fill: '#fff', opacity: '0.7' }),
      ])
  },
})

// Map platform name -> icon component
export const platformIconMap: Record<string, any> = {
  'Netflix': NetflixIcon,
  'Disney+': DisneyPlusIcon,
  'HBO': HBOIcon,
  'Apple TV+': AppleTVIcon,
  'Amazon': AmazonIcon,
  'Hulu': HuluIcon,
  'Paramount+': ParamountIcon,
  'Peacock': PeacockIcon,
  'Crunchyroll': CrunchyrollIcon,
}

export function getPlatformIcon(name: string) {
  return platformIconMap[name] || GenericPlatformIcon
}
