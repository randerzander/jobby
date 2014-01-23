var jobby = angular.module('jobby', []);

jobby.controller('runningJobsCtrl', function($scope, $http){
  $http.get('status').success(function(data){
    runningJobs = [];
    $.each(data, function(i, v){
      j = {job: v.Job};
      j.startTime = Math.round(v.Start/1000000);
      j.endTime = j.startTime + Math.round(v.Average/1000000);
      j.startTime = new Date(j.startTime);
      j.endTime = new Date(j.endTime);
      j.startTime = j.startTime.toLocaleDateString() + ' at ' + j.startTime.toLocaleTimeString();
      j.endTime = j.endTime.toLocaleDateString() + ' at ' + j.endTime.toLocaleTimeString();
      j.ETC = (j.endTime - new Date())/1000/60;
      runningJobs.push(j);
    });
    $scope.runningJobs = runningJobs;
  });
});
