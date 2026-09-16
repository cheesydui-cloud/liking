(function () {
  var t = localStorage.getItem('lk-theme')
  var dark = t === 'dark'
  document.documentElement.classList.toggle('dark', dark)
  var m = document.querySelector('meta[name="theme-color"]')
  if (m) m.setAttribute('content', dark ? '#18181B' : '#F6F6F4')
})()
